package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/radiergummi/cetacean/internal/audit"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/exec"
)

const (
	execWriteTimeout = 10 * time.Second
	execCloseTimeout = 10 * time.Second
	execReadLimit    = 64 << 10
	execMaxWinsize   = 1<<16 - 1 // a TTY's winsize fields are uint16
)

var errStartTimeout = errors.New("the shell did not start in time")

type execStatusFrame struct {
	Type  string `json:"type"`
	Phase string `json:"phase"`
}

// execExitFrame carries a code only when the shell exited on its own, and a
// reason only when it did not.
type execExitFrame struct {
	Type    string       `json:"type"`
	Code    *int         `json:"code,omitempty"`
	Reason  string       `json:"reason,omitempty"`
	Outcome exec.Outcome `json:"outcome"`
}

type execErrorFrame struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type execResizeFrame struct {
	Type string `json:"type"`
	Cols uint   `json:"cols"`
	Rows uint   `json:"rows"`
}

func execExit(s exec.Session) execExitFrame {
	frame := execExitFrame{Type: "exit", Reason: s.Reason}
	if s.Exit != nil {
		frame.Outcome = s.Exit.Outcome
		if s.Exit.Outcome == exec.OutcomeExited {
			frame.Code, frame.Reason = new(s.Exit.Code), ""
		}
	}

	return frame
}

// isWebSocketUpgrade reads Upgrade as the token list RFC 9110 makes it.
func isWebSocketUpgrade(r *http.Request) bool {
	for _, value := range r.Header.Values("Upgrade") {
		for token := range strings.SplitSeq(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "websocket") {
				return true
			}
		}
	}

	return false
}

// execOriginAllowed is RFC 6455 §10.2's origin check: the rule writes pass, which
// Check waives for a GET, without the "none" of a navigation the user started.
// A browser never sends that on a handshake. Check compares bytes, and an
// origin's scheme and host are case-insensitive.
func (h *Handlers) execOriginAllowed(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "none" {
		return false
	}

	asWrite := r.WithContext(r.Context())
	asWrite.Method = http.MethodPost
	asWrite.Host = strings.ToLower(r.Host)
	asWrite.Header = r.Header.Clone()
	asWrite.Header.Set("Origin", strings.ToLower(r.Header.Get("Origin")))

	return h.crossOrigin.Check(asWrite) == nil
}

func (h *Handlers) attachExecSession(w http.ResponseWriter, r *http.Request) {
	m, sid := h.exec.Manager, r.PathValue("sid")
	id := auth.IdentityFromContext(r.Context())

	if !h.execOriginAllowed(r) {
		h.refuseSession(w, r, "CSR001", "this origin may not open a terminal")
		return
	}

	s, ok := m.Get(sid)
	if !ok || !h.canSeeSession(r, s) {
		h.refuseSession(w, r, "EXE006", "exec session not found")
		return
	}

	ctx, err := m.Attach(s.ID, id)
	switch {
	case errors.Is(err, exec.ErrNotOwner):
		h.refuseSession(w, r, "EXE004", err.Error())
		return
	case err != nil:
		h.refuseSession(w, r, "EXE005", err.Error())
		return
	}

	transport, ok := exec.Select(h.exec.Transports, s.Target.Node)
	if !ok {
		m.Abort(s.ID, "relay_unavailable")
		h.refuseSession(w, r, "EXE002", "no shell transport reaches node "+s.Target.Node)
		return
	}

	// Hijack may leave the server's ReadTimeout on the connection, and a
	// terminal is read from for hours.
	_ = http.NewResponseController(w).SetReadDeadline(time.Time{})

	// The origin was checked before Attach, which a refusal here would consume.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		m.Abort(s.ID, "upgrade_failed")
		return
	}
	conn.SetReadLimit(execReadLimit)

	h.runExecSession(ctx, conn, s, transport)
}

// openShell bounds Open, which may not honour ctx. A shell that opens after it
// gave up is closed rather than left running.
func (h *Handlers) openShell(
	ctx context.Context,
	s exec.Session,
	transport exec.Transport,
) (exec.Stream, error) {
	type result struct {
		stream exec.Stream
		err    error
	}
	opened := make(chan result, 1)
	go func() {
		stream, err := transport.Open(ctx, exec.Request{
			ContainerID: s.Target.Container, NodeID: s.Target.Node, Cols: 80, Rows: 24,
		})
		opened <- result{stream, err}
	}()

	timeout := h.exec.startTimeout
	if timeout == 0 {
		timeout = exec.AttachWindow
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var err error
	select {
	case o := <-opened:
		return o.stream, o.err
	case <-timer.C:
		err = errStartTimeout
	case <-ctx.Done():
		err = ctx.Err()
	}

	go func() {
		if o := <-opened; o.err == nil {
			closeStream(o.stream)
		}
	}()

	return nil, err
}

func closeStream(stream exec.Stream) exec.Exit {
	ctx, cancel := context.WithTimeout(context.Background(), execCloseTimeout)
	defer cancel()

	return stream.Close(ctx)
}

// runExecSession pumps one terminal. ctx is the session's and is cancelled by
// whoever ends it; the connection is never read or written under that context,
// because coder/websocket closes a connection whose operation is cancelled.
// Every return ends the session through exactly one Abort or Finish.
func (h *Handlers) runExecSession(
	ctx context.Context,
	conn *websocket.Conn,
	s exec.Session,
	transport exec.Transport,
) {
	m := h.exec.Manager
	connCtx, closeConn := context.WithCancel(context.Background())
	defer closeConn()

	send := func(v any) {
		b, _ := json.Marshal(v)
		writeCtx, cancel := context.WithTimeout(connCtx, execWriteTimeout)
		defer cancel()
		_ = conn.Write(writeCtx, websocket.MessageText, b)
	}
	// stop ends a session whose shell never ran. A reason someone else gave
	// first wins, and the terminal is told that one as an exit.
	stop := func(reason, message string) {
		m.Abort(s.ID, reason)
		if ended, ok := m.Get(s.ID); ok && ended.Reason != reason {
			send(execExit(ended))
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		}
		send(execErrorFrame{Type: "error", Code: reason, Message: message})
		_ = conn.Close(websocket.StatusInternalError, reason)
	}

	send(execStatusFrame{Type: "status", Phase: "connecting"})

	stream, err := h.openShell(ctx, s, transport)
	switch {
	case err == nil:
	case errors.Is(err, errStartTimeout):
		stop("start_timeout", "the shell did not start in time")
		return
	case errors.Is(err, exec.ErrNoShell):
		stop("no_shell", "this container has no /bin/sh")
		return
	default:
		if ctx.Err() == nil {
			slog.WarnContext(ctx, "exec shell not opened", "session", s.ID, "error", err)
		}
		stop("open_failed", "the shell could not be started")
		return
	}

	if err := m.Connected(s.ID); err != nil {
		closeStream(stream)
		if errors.Is(err, audit.ErrUnavailable) {
			stop("audit_unavailable", "the shell could not be recorded")
		} else {
			stop("not_started", "the session ended before its shell started")
		}
		return
	}
	send(execStatusFrame{Type: "status", Phase: "connected"})

	// A shell that stops answering ends the pump without a reason: only the
	// outcome tells an exit from a lost connection.
	pumpCtx, endPump := context.WithCancel(ctx)
	defer endPump()

	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		buf := make([]byte, 32<<10)
		for {
			n, err := stream.Read(buf)
			if n > 0 {
				m.Output(s.ID, n)
				writeCtx, cancel := context.WithTimeout(connCtx, execWriteTimeout)
				werr := conn.Write(writeCtx, websocket.MessageBinary, buf[:n])
				cancel()
				if werr != nil {
					m.End(s.ID, "disconnected", nil)
					return
				}
			}
			if err != nil {
				endPump()
				return
			}
		}
	}()

	go func() {
		for {
			typ, data, err := conn.Read(connCtx)
			if err != nil {
				m.End(s.ID, "disconnected", nil)
				return
			}
			switch typ {
			case websocket.MessageBinary:
				m.Input(s.ID, len(data))
				if _, err := stream.Write(data); err != nil {
					endPump()
					return
				}
			case websocket.MessageText:
				var frame execResizeFrame
				if json.Unmarshal(data, &frame) == nil && frame.Type == "resize" &&
					frame.Cols > 0 && frame.Rows > 0 {
					_ = stream.Resize(connCtx, min(frame.Cols, execMaxWinsize),
						min(frame.Rows, execMaxWinsize))
				}
			}
		}
	}()

	<-pumpCtx.Done()

	exit := closeStream(stream)
	<-outputDone
	if exit.Outcome != exec.OutcomeExited {
		m.End(s.ID, "connection_lost", nil)
	}

	send(execExit(m.Finish(s.ID, exit)))
	_ = conn.Close(websocket.StatusNormalClosure, "")
}
