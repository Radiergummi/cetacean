package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/radiergummi/cetacean/internal/audit"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/exec"
	"github.com/radiergummi/cetacean/internal/spec"
)

// auditRecorder hands every record to the test, and refuses the event named
// by fail as an unwritable audit log would.
type auditRecorder struct {
	events chan audit.Record
	fail   string
}

func (a auditRecorder) Write(r audit.Record) error {
	if r.Event == a.fail {
		return audit.ErrUnavailable
	}
	a.events <- r
	return nil
}

type wsConfig struct {
	readTimeout  time.Duration
	transport    exec.Transport
	failEvent    string
	startTimeout time.Duration
	routerOpts   []routerOption
}

type wsHarness struct {
	server *httptest.Server
	deps   ExecDeps
	stream *fakeStream
	audit  auditRecorder
}

func newWSHarness(t *testing.T, cfg wsConfig) *wsHarness {
	t.Helper()
	stream := newFakeStream()
	if cfg.transport == nil {
		cfg.transport = &fakeTransport{node: "n1", stream: stream}
	}
	rec := auditRecorder{events: make(chan audit.Record, 64), fail: cfg.failEvent}
	deps := newExecDeps(cfg.transport)
	deps.startTimeout = cfg.startTimeout
	deps.Manager = exec.NewManager(exec.ManagerConfig{
		Limits: exec.Limits{
			MaxSessions: 4, MaxPerIdentity: 2, IdleTimeout: time.Hour, MaxDuration: time.Hour,
		},
		Audit: rec,
	})

	server := httptest.NewUnstartedServer(newTestRouterWithConfig(t, cfg.routerOpts,
		withCache(execCache()), withOpsLevel(config.OpsImpactful),
		withACL(execPolicy("read", "exec")), withExec(deps)))
	server.Config.ReadTimeout = cfg.readTimeout
	server.Start()
	t.Cleanup(server.Close)

	return &wsHarness{server: server, deps: deps, stream: stream, audit: rec}
}

func (h *wsHarness) create(t *testing.T) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/tasks/t1/exec", nil)
	req.Header.Set("Accept", "application/json")
	resp, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d", resp.StatusCode)
	}
	return resp.Header.Get("Location")
}

// dial upgrades loc, and reports the handshake's status when it was refused.
func (h *wsHarness) dial(loc, origin string) (*websocket.Conn, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var opts *websocket.DialOptions
	if origin != "" {
		opts = &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}}}
	}
	conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(h.server.URL, "http")+loc, opts)
	if resp == nil {
		return conn, 0, err
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	return conn, resp.StatusCode, err
}

func (h *wsHarness) open(t *testing.T) (*websocket.Conn, string) {
	t.Helper()
	loc := h.create(t)
	conn, _, err := h.dial(loc, "")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })

	return conn, strings.TrimPrefix(loc, "/exec/")
}

func readFrame(t *testing.T, conn *websocket.Conn) (websocket.MessageType, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return typ, data
}

// expectFrame reads a text frame and requires exactly want's fields.
func expectFrame(t *testing.T, conn *websocket.Conn, want map[string]any) {
	t.Helper()
	typ, data := readFrame(t, conn)
	if typ != websocket.MessageText {
		t.Fatalf("got a binary frame %q, want %v", data, want)
	}
	var got map[string]any
	_ = json.Unmarshal(data, &got)
	if len(got) != len(want) {
		t.Fatalf("frame = %s, want %v", data, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("frame = %s, want %s=%v", data, k, v)
		}
	}
}

// expectError reads an error frame with the given code and any message.
func expectError(t *testing.T, conn *websocket.Conn, code string) {
	t.Helper()
	_, data := readFrame(t, conn)
	var got map[string]any
	_ = json.Unmarshal(data, &got)
	if got["type"] != "error" || got["code"] != code || got["message"] == "" || len(got) != 3 {
		t.Fatalf("frame = %s, want an error %s", data, code)
	}
}

var (
	connecting = map[string]any{"type": "status", "phase": "connecting"}
	connected  = map[string]any{"type": "status", "phase": "connected"}
)

func waitEnded(t *testing.T, m *exec.Manager, sid string) exec.Session {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s, _ := m.Get(sid); s.State == exec.StateEnded {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	s, _ := m.Get(sid)
	t.Fatalf("session never ended: %+v", s)
	return s
}

func TestExecWebSocketEchoesAndResizes(t *testing.T) {
	h := newWSHarness(t, wsConfig{})
	conn, _ := h.open(t)
	expectFrame(t, conn, connecting)
	expectFrame(t, conn, connected)

	ctx := context.Background()
	_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":120,"rows":40}`))
	_ = conn.Write(ctx, websocket.MessageBinary, []byte("ls\r"))
	if typ, data := readFrame(t, conn); typ != websocket.MessageBinary || string(data) != "ls\r" {
		t.Fatalf("echo = %v %q", typ, data)
	}

	h.stream.mu.Lock()
	defer h.stream.mu.Unlock()
	if len(h.stream.resizes) != 1 || h.stream.resizes[0] != [2]uint{120, 40} {
		t.Errorf("resizes = %v", h.stream.resizes)
	}
}

func TestExecWebSocketShellExitSendsExitFrame(t *testing.T) {
	for _, code := range []int{0, 3} {
		h := newWSHarness(t, wsConfig{})
		conn, sid := h.open(t)
		expectFrame(t, conn, connecting)
		expectFrame(t, conn, connected)

		h.stream.closeShell(code)
		expectFrame(t, conn, map[string]any{
			"type": "exit", "code": float64(code), "outcome": "exited",
		})

		s := waitEnded(t, h.deps.Manager, sid)
		if s.Reason != "exited" || s.Exit.Code != code || s.Exit.Outcome != exec.OutcomeExited {
			t.Errorf("session = %+v exit %+v", s, s.Exit)
		}
	}
}

func TestExecWebSocketEndedSessionSendsReasonWithoutCode(t *testing.T) {
	h := newWSHarness(t, wsConfig{})
	conn, sid := h.open(t)
	expectFrame(t, conn, connecting)
	expectFrame(t, conn, connected)

	h.deps.Manager.End(sid, "terminated_by", &auth.Identity{Subject: "bob", Provider: "oidc"})
	expectFrame(t, conn, map[string]any{
		"type": "exit", "reason": "terminated_by", "outcome": "terminated",
	})
}

func TestExecWebSocketDisconnectEndsSessionOnce(t *testing.T) {
	h := newWSHarness(t, wsConfig{})
	conn, sid := h.open(t)
	expectFrame(t, conn, connecting)
	expectFrame(t, conn, connected)
	if r := <-h.audit.events; r.Event != "exec.open" {
		t.Fatalf("first record = %s", r.Event)
	}
	if r := <-h.audit.events; r.Event != "exec.start" {
		t.Fatalf("second record = %s", r.Event)
	}

	_ = conn.CloseNow()

	select {
	case r := <-h.audit.events:
		if r.Event != "exec.end" || r.Reason != "disconnected" || r.Outcome != "terminated" {
			t.Errorf("end record = %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no end record after the client vanished")
	}
	select {
	case r := <-h.audit.events:
		t.Errorf("a second record was written: %+v", r)
	case <-time.After(200 * time.Millisecond):
	}

	h.stream.mu.Lock()
	defer h.stream.mu.Unlock()
	if h.stream.closed != 1 {
		t.Errorf("stream closed %d times", h.stream.closed)
	}
	if s, _ := h.deps.Manager.Get(sid); s.State != exec.StateEnded {
		t.Errorf("session = %+v", s)
	}
}

func TestExecWebSocketSurvivesServerReadTimeout(t *testing.T) {
	h := newWSHarness(t, wsConfig{readTimeout: 200 * time.Millisecond})
	conn, _ := h.open(t)
	expectFrame(t, conn, connecting)
	expectFrame(t, conn, connected)

	time.Sleep(600 * time.Millisecond)
	_ = conn.Write(context.Background(), websocket.MessageBinary, []byte("x"))
	if _, data := readFrame(t, conn); string(data) != "x" {
		t.Fatalf("echo after the read timeout = %q", data)
	}
}

func TestExecWebSocketAttachesOnlyOnce(t *testing.T) {
	h := newWSHarness(t, wsConfig{})
	_, sid := h.open(t)

	_, status, err := h.dial("/exec/"+sid, "")
	if err == nil || status != http.StatusConflict {
		t.Fatalf("second attach = %v %d, want 409", err, status)
	}
}

func TestExecWebSocketChecksOriginBeforeAttaching(t *testing.T) {
	spec.Satisfies(t,
		"http/rfc6455/servers-verify-the-origin",
		"http/rfc6455/an-unacceptable-origin-gets-403",
	)

	h := newWSHarness(t, wsConfig{routerOpts: []routerOption{
		withCORS("https://a.example"),
		func(cfg *RouterConfig) { cfg.PublicURL = "https://cetacean.example" },
	}})

	loc := h.create(t)
	for _, origin := range []string{"https://evil.example", "http://a.example", "null"} {
		_, status, err := h.dial(loc, origin)
		if err == nil || status != http.StatusForbidden {
			t.Fatalf("origin %s = %v %d, want 403", origin, err, status)
		}
	}
	if s, _ := h.deps.Manager.Get(strings.TrimPrefix(loc, "/exec/")); s.State != exec.StatePending {
		t.Fatalf("a refused origin ended the session: %+v", s)
	}

	for _, origin := range []string{
		"", h.server.URL, "https://A.example", "https://cetacean.example",
	} {
		h.deps.Manager.End(strings.TrimPrefix(loc, "/exec/"), "test", nil)
		loc = h.create(t)
		conn, _, err := h.dial(loc, origin)
		if err != nil {
			t.Fatalf("origin %s: %v", origin, err)
		}
		expectFrame(t, conn, connecting)
		_ = conn.CloseNow()
	}
}

func TestExecWebSocketRefusesUnstartableShells(t *testing.T) {
	cases := map[string]struct {
		cfg  wsConfig
		code string
	}{
		"no shell": {
			wsConfig{transport: &fakeTransport{node: "n1", err: exec.ErrNoShell}},
			"no_shell",
		},
		"open failed": {
			wsConfig{transport: &fakeTransport{node: "n1", err: errors.New("boom")}},
			"open_failed",
		},
		"audit failed": {wsConfig{failEvent: "exec.start"}, "audit_unavailable"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newWSHarness(t, tc.cfg)
			conn, sid := h.open(t)
			expectFrame(t, conn, connecting)
			expectError(t, conn, tc.code)

			s := waitEnded(t, h.deps.Manager, sid)
			if s.Reason != tc.code || s.Exit.Outcome != exec.OutcomeDetached || s.Exit.Code != -1 {
				t.Errorf("session = %+v exit %+v", s, s.Exit)
			}
			for _, want := range []string{"exec.open", "exec.end"} {
				select {
				case r := <-h.audit.events:
					if r.Event != want {
						t.Errorf("record = %+v, want %s", r, want)
					}
					if want == "exec.end" && r.Reason != tc.code {
						t.Errorf("end record = %+v, want reason %s", r, tc.code)
					}
				case <-time.After(5 * time.Second):
					t.Fatalf("no %s record", want)
				}
			}
		})
	}
}

// gatedTransport opens a stream only once release is closed, ignoring ctx as
// a transport that hangs would.
type gatedTransport struct {
	fakeTransport
	release chan struct{}
}

func (g *gatedTransport) Open(context.Context, exec.Request) (exec.Stream, error) {
	<-g.release
	return g.stream, nil
}

func TestExecWebSocketBoundsTheStart(t *testing.T) {
	stream := newFakeStream()
	gate := &gatedTransport{fakeTransport{node: "n1", stream: stream}, make(chan struct{})}
	h := newWSHarness(t, wsConfig{transport: gate, startTimeout: 100 * time.Millisecond})
	conn, sid := h.open(t)
	expectFrame(t, conn, connecting)
	expectError(t, conn, "start_timeout")

	s := waitEnded(t, h.deps.Manager, sid)
	if s.Reason != "start_timeout" || s.Exit.Outcome != exec.OutcomeDetached {
		t.Errorf("session = %+v exit %+v", s, s.Exit)
	}

	close(gate.release)
	waitClosedOnce(t, stream)
}

func TestExecWebSocketEndedWhileStartingSendsExit(t *testing.T) {
	stream := newFakeStream()
	gate := &gatedTransport{fakeTransport{node: "n1", stream: stream}, make(chan struct{})}
	h := newWSHarness(t, wsConfig{transport: gate})
	conn, sid := h.open(t)
	expectFrame(t, conn, connecting)

	h.deps.Manager.End(sid, "terminated_by", &auth.Identity{Subject: "bob", Provider: "oidc"})
	close(gate.release)
	expectFrame(t, conn, map[string]any{
		"type": "exit", "reason": "terminated_by", "outcome": "detached",
	})

	waitClosedOnce(t, stream)
}

// waitClosedOnce requires a stream the pump let go of to be closed exactly once.
func waitClosedOnce(t *testing.T, stream *fakeStream) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		stream.mu.Lock()
		closed := stream.closed
		stream.mu.Unlock()
		if closed == 1 {
			break
		}
		if closed > 1 || time.Now().After(deadline) {
			t.Fatalf("stream closed %d times", closed)
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.closed != 1 {
		t.Fatalf("stream closed %d times", stream.closed)
	}
}

func TestExecWebSocketClampsResizeToAWinsize(t *testing.T) {
	h := newWSHarness(t, wsConfig{})
	conn, _ := h.open(t)
	expectFrame(t, conn, connecting)
	expectFrame(t, conn, connected)

	ctx := context.Background()
	_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":70000,"rows":40}`))
	_ = conn.Write(ctx, websocket.MessageBinary, []byte("x"))
	readFrame(t, conn)

	h.stream.mu.Lock()
	defer h.stream.mu.Unlock()
	if len(h.stream.resizes) != 1 || h.stream.resizes[0] != [2]uint{65535, 40} {
		t.Errorf("resizes = %v", h.stream.resizes)
	}
}

// droppingStream loses its output while the shell is still running, as a
// dropped daemon connection does.
type droppingStream struct {
	*fakeStream
	drop chan struct{}
}

func (d *droppingStream) Read(p []byte) (int, error) {
	select {
	case b := <-d.out:
		return copy(p, b), nil
	case <-d.drop:
		return 0, io.ErrUnexpectedEOF
	}
}

type droppingTransport struct {
	fakeTransport
	dropping *droppingStream
}

func (d *droppingTransport) Open(context.Context, exec.Request) (exec.Stream, error) {
	return d.dropping, nil
}

func TestExecWebSocketLostShellSendsReasonWithoutCode(t *testing.T) {
	dropping := &droppingStream{newFakeStream(), make(chan struct{})}
	h := newWSHarness(t, wsConfig{
		transport: &droppingTransport{fakeTransport{node: "n1"}, dropping},
	})
	conn, _ := h.open(t)
	expectFrame(t, conn, connecting)
	expectFrame(t, conn, connected)

	close(dropping.drop)
	expectFrame(t, conn, map[string]any{
		"type": "exit", "reason": "connection_lost", "outcome": "terminated",
	})
}

func TestExecOriginAllowedReadsSecFetchSiteFirst(t *testing.T) {
	spec.Satisfies(t, "http/rfc6455/servers-verify-the-origin")

	h := &Handlers{crossOrigin: http.NewCrossOriginProtection()}
	if err := h.crossOrigin.AddTrustedOrigin("https://a.example"); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, origin, fetchSite string
		want                    bool
	}{
		// Behind a proxy that rewrites Host, Origin names the public host.
		{"same-origin behind a proxy", "https://cetacean.example", "same-origin", true},
		{"user-initiated", "https://cetacean.example", "none", false},
		{"cross-site, trusted", "https://a.example", "cross-site", true},
		{"cross-site, untrusted", "https://evil.example", "cross-site", false},
		{"cross-site naming the host", "http://backend:9000", "cross-site", false},
		{"same-site, untrusted", "https://sub.cetacean.example", "same-site", false},
		{"no fetch metadata, host", "http://backend:9000", "", true},
		{"no fetch metadata, foreign", "https://cetacean.example", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://backend:9000/exec/s1", nil)
			r.Header.Set("Origin", tc.origin)
			if tc.fetchSite != "" {
				r.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			}
			if got := h.execOriginAllowed(r); got != tc.want {
				t.Errorf("execOriginAllowed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExecOriginAllowedLeavesTheRequestAlone(t *testing.T) {
	h := &Handlers{crossOrigin: http.NewCrossOriginProtection()}
	r := httptest.NewRequest(http.MethodGet, "/exec/s1", nil)
	if !h.execOriginAllowed(r) {
		t.Error("a client that is not a browser was refused")
	}
	if r.Method != http.MethodGet {
		t.Errorf("method = %s, want GET", r.Method)
	}
}
