package exec

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
)

type DockerExec interface {
	ContainerStatPath(ctx context.Context, containerID, path string) (container.PathStat, error)
	ContainerExecCreate(
		ctx context.Context,
		containerID string,
		options container.ExecOptions,
	) (container.ExecCreateResponse, error)
	ContainerExecAttach(
		ctx context.Context,
		execID string,
		options container.ExecAttachOptions,
	) (types.HijackedResponse, error)
	ContainerExecStart(ctx context.Context, execID string, options container.ExecStartOptions) error
	ContainerExecResize(ctx context.Context, execID string, options container.ResizeOptions) error
	ContainerExecInspect(ctx context.Context, execID string) (container.ExecInspect, error)
}

// startScript prints the shell's PID inside the container first: the Engine API
// has no way to kill an exec, so ending one means signalling that PID.
const startScript = `echo "$$"; command -v bash >/dev/null 2>&1 && exec bash; exec sh`

const pidLineTimeout = 10 * time.Second

// Local opens shells through Cetacean's own Docker client, which reaches only
// the containers on the node that daemon runs.
type Local struct {
	Docker DockerExec
	NodeID func() string
	// Grace bounds how long Close waits after each signal for the shell to
	// report it stopped before escalating or giving up. Zero means 2s.
	Grace time.Duration
	// SettlePollWindow and SettlePollStep bound how long Close, after output
	// EOF, polls ExecInspect for a shell that exited on its own before
	// sending HUP. Zero means 300ms / 25ms.
	SettlePollWindow time.Duration
	SettlePollStep   time.Duration
}

func (l *Local) Name() string { return "local" }

func (l *Local) Reaches(nodeID string) bool {
	return nodeID != "" && nodeID == l.NodeID()
}

func (l *Local) Open(ctx context.Context, req Request) (Stream, error) {
	if _, err := l.Docker.ContainerStatPath(ctx, req.ContainerID, "/bin/sh"); err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil, ErrNoShell
		}

		return nil, err
	}

	var size *[2]uint
	if req.Cols > 0 && req.Rows > 0 {
		size = &[2]uint{req.Rows, req.Cols}
	}

	created, err := l.Docker.ContainerExecCreate(ctx, req.ContainerID, container.ExecOptions{
		Cmd:          []string{"/bin/sh", "-c", startScript},
		Tty:          true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		ConsoleSize:  size,
	})
	if err != nil {
		return nil, err
	}

	attached, err := l.Docker.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{
		Tty:         true,
		ConsoleSize: size,
	})
	if err != nil {
		return nil, err
	}

	_ = attached.Conn.SetReadDeadline(time.Now().Add(pidLineTimeout))
	line, err := attached.Reader.ReadString('\n')
	_ = attached.Conn.SetReadDeadline(time.Time{})
	if err != nil {
		attached.Close()
		return nil, fmt.Errorf("shell did not report its pid: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		attached.Close()
		return nil, fmt.Errorf("shell reported %q instead of its pid", line)
	}

	grace := l.Grace
	if grace == 0 {
		grace = 2 * time.Second
	}

	settleWindow := l.SettlePollWindow
	if settleWindow == 0 {
		settleWindow = 300 * time.Millisecond
	}

	settleStep := l.SettlePollStep
	if settleStep == 0 {
		settleStep = 25 * time.Millisecond
	}

	return &localStream{
		docker: l.Docker, container: req.ContainerID, execID: created.ID,
		pid: pid, attached: attached,
		grace: grace, settleWindow: settleWindow, settleStep: settleStep,
	}, nil
}

type localStream struct {
	docker       DockerExec
	container    string
	execID       string
	pid          int
	attached     types.HijackedResponse
	grace        time.Duration
	settleWindow time.Duration
	settleStep   time.Duration

	eof atomic.Bool

	closeOnce   sync.Once
	closeResult Exit
}

func (s *localStream) Read(p []byte) (int, error) {
	n, err := s.attached.Reader.Read(p)
	if err != nil {
		s.eof.Store(true)
	}

	return n, err
}

func (s *localStream) Write(p []byte) (int, error) { return s.attached.Conn.Write(p) }

func (s *localStream) Resize(ctx context.Context, cols, rows uint) error {
	return s.docker.ContainerExecResize(ctx, s.execID, container.ResizeOptions{
		Height: rows, Width: cols,
	})
}

// Close ends the shell at most once: concurrent and later callers block on, or
// replay, the first call's result rather than re-running it.
func (s *localStream) Close(ctx context.Context) Exit {
	s.closeOnce.Do(func() {
		s.closeResult = s.close(ctx)
	})

	return s.closeResult
}

// close ignores the caller's cancellation so a disconnect can still signal the
// shell, but bounds itself so it can't hang forever in its place.
func (s *localStream) close(ctx context.Context) Exit {
	defer s.attached.Close()

	budget := s.settleWindow + 2*s.grace + time.Second
	workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), budget)
	defer cancel()

	// Only output EOF means the shell itself is gone; without it, a signal
	// is sent at once rather than waiting out the settle window on a shell
	// that may still be live and attached.
	if s.eof.Load() {
		if exit, done := s.poll(workCtx, s.settleWindow, s.settleStep); done {
			return exit
		}
	} else if exit, done := s.finished(workCtx); done {
		return exit
	}

	for _, signal := range []string{"HUP", "KILL"} {
		s.signal(workCtx, signal)
		if exit, done := s.poll(workCtx, s.grace, s.grace/10); done {
			exit.Outcome = OutcomeTerminated
			return exit
		}
	}

	return Exit{Code: -1, Outcome: OutcomeDetached}
}

// poll checks finished every step until it reports done or window elapses.
func (s *localStream) poll(ctx context.Context, window, step time.Duration) (Exit, bool) {
	deadline := time.Now().Add(window)
	for {
		if exit, done := s.finished(ctx); done {
			return exit, true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return Exit{}, false
		}
		time.Sleep(step)
	}
}

func (s *localStream) finished(ctx context.Context) (Exit, bool) {
	inspect, err := s.docker.ContainerExecInspect(ctx, s.execID)
	if err != nil || inspect.Running {
		return Exit{}, false
	}

	return Exit{Code: inspect.ExitCode, Outcome: OutcomeExited}, true
}

func (s *localStream) signal(ctx context.Context, signal string) {
	created, err := s.docker.ContainerExecCreate(ctx, s.container, container.ExecOptions{
		Cmd: []string{"/bin/sh", "-c", "kill -" + signal + " " + strconv.Itoa(s.pid)},
	})
	if err != nil {
		return
	}

	_ = s.docker.ContainerExecStart(ctx, created.ID, container.ExecStartOptions{Detach: true})
}
