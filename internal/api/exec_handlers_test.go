package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/audit"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/exec"
)

// fakeStream is a shell that echoes input and ends when closeShell is called.
type fakeStream struct {
	out      chan []byte
	mu       sync.Mutex
	resizes  [][2]uint
	closed   int
	exitCode int
	done     chan struct{}
	once     sync.Once
}

func newFakeStream() *fakeStream {
	return &fakeStream{out: make(chan []byte, 16), done: make(chan struct{})}
}

func (s *fakeStream) Read(p []byte) (int, error) {
	select {
	case b := <-s.out:
		return copy(p, b), nil
	case <-s.done:
		return 0, io.EOF
	}
}

func (s *fakeStream) Write(p []byte) (int, error) {
	s.out <- append([]byte(nil), p...)
	return len(p), nil
}

func (s *fakeStream) Resize(_ context.Context, cols, rows uint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resizes = append(s.resizes, [2]uint{cols, rows})
	return nil
}

func (s *fakeStream) closeShell(code int) {
	s.mu.Lock()
	s.exitCode = code
	s.mu.Unlock()
	s.once.Do(func() { close(s.done) })
}

func (s *fakeStream) Close(context.Context) exec.Exit {
	s.mu.Lock()
	s.closed++
	code := s.exitCode
	s.mu.Unlock()
	select {
	case <-s.done:
		return exec.Exit{Code: code, Outcome: exec.OutcomeExited}
	default:
		s.closeShell(129)
		return exec.Exit{Code: 129, Outcome: exec.OutcomeTerminated}
	}
}

type fakeTransport struct {
	node   string
	stream *fakeStream
	err    error
}

func (f *fakeTransport) Name() string             { return "local" }
func (f *fakeTransport) Reaches(node string) bool { return node == f.node }
func (f *fakeTransport) Open(context.Context, exec.Request) (exec.Stream, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.stream, nil
}

func execCache() *cache.Cache {
	c := cache.New(nil)
	node := swarm.Node{ID: "n1"}
	node.Status.State = swarm.NodeStateReady
	node.Spec.Availability = swarm.NodeAvailabilityActive
	node.Description.Platform.OS = "linux"
	c.SetNode(node)
	svc := swarm.Service{ID: "s1"}
	svc.Spec.Name = "web"
	c.SetService(svc)
	c.SetTask(swarm.Task{
		ID: "t1", ServiceID: "s1", NodeID: "n1",
		Status: swarm.TaskStatus{
			State:           swarm.TaskStateRunning,
			ContainerStatus: &swarm.ContainerStatus{ContainerID: "c1"},
		},
	})
	c.SetTask(swarm.Task{
		ID: "t2", ServiceID: "s1", NodeID: "n1",
		Status: swarm.TaskStatus{State: swarm.TaskStateShutdown},
	})
	return c
}

func execPolicy(permissions ...string) *acl.Evaluator {
	return execPolicyOn([]string{"*"}, permissions...)
}

func execPolicyOn(resources []string, permissions ...string) *acl.Evaluator {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
		{Resources: resources, Audience: []string{"*"}, Permissions: permissions},
	}})
	return e
}

func newExecDeps(transport exec.Transport) ExecDeps {
	return ExecDeps{
		Manager: exec.NewManager(exec.ManagerConfig{Limits: exec.Limits{
			MaxSessions: 4, MaxPerIdentity: 2, IdleTimeout: time.Hour, MaxDuration: time.Hour,
		}}),
		Transports: []exec.Transport{transport},
		Self:       func(swarm.Task) bool { return false },
	}
}

// specExecDeps enables exec on the routers the spec walks drive, with one
// session the anonymous caller owns so /exec lists something to validate.
func specExecDeps(t testing.TB) ExecDeps {
	t.Helper()
	deps := newExecDeps(&fakeTransport{node: "node1"})
	_, err := deps.Manager.Create(&auth.Identity{Subject: "anonymous", Provider: "none"}, "",
		exec.Target{Task: "task1", Service: "svc1", ServiceName: "web", Node: "node1"}, "local")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return deps
}

func execRouter(t *testing.T, deps ExecDeps, opts ...testHandlersOption) http.Handler {
	t.Helper()
	opts = append([]testHandlersOption{
		withCache(execCache()), withOpsLevel(config.OpsImpactful),
		withACL(execPolicy("read", "exec")), withExec(deps),
	}, opts...)
	return newTestRouterWithConfig(t, nil, opts...)
}

func do(t *testing.T, h http.Handler, method, path, accept string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func aliceSession(t *testing.T, deps ExecDeps) exec.Session {
	t.Helper()
	s, err := deps.Manager.Create(&auth.Identity{Subject: "alice", Provider: "oidc"}, "",
		exec.Target{Task: "t1", Service: "s1", ServiceName: "web", Node: "n1", Container: "c1"},
		"local")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return s
}

func TestTaskExecAllowCarriesPOSTOnlyWhenEverythingPasses(t *testing.T) {
	deps := newExecDeps(&fakeTransport{node: "n1", stream: newFakeStream()})

	cases := map[string]struct {
		opts     []testHandlersOption
		deps     ExecDeps
		path     string
		wantPOST bool
	}{
		"allowed": {nil, deps, "/tasks/t1/exec", true},
		"write is not exec": {
			[]testHandlersOption{withACL(execPolicy("read", "write"))},
			deps,
			"/tasks/t1/exec",
			false,
		},
		"level too low": {
			[]testHandlersOption{withOpsLevel(config.OpsConfiguration)},
			deps,
			"/tasks/t1/exec",
			false,
		},
		"not eligible": {nil, deps, "/tasks/t2/exec", false},
		"no transport": {
			nil,
			newExecDeps(&fakeTransport{node: "elsewhere"}),
			"/tasks/t1/exec",
			false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := do(
				t,
				execRouter(t, tc.deps, tc.opts...),
				http.MethodGet,
				tc.path,
				"application/json",
			)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", w.Code, w.Body)
			}
			if got := strings.Contains(w.Header().Get("Allow"), "POST"); got != tc.wantPOST {
				t.Errorf("Allow = %q, want POST: %v", w.Header().Get("Allow"), tc.wantPOST)
			}
		})
	}
}

// Exec resolves a task to its service the way task writes do, so a grant
// naming the service reaches the shells in its tasks.
func TestExecGrantOnTheServiceCoversItsTasks(t *testing.T) {
	cases := map[string]struct {
		resources []string
		want      bool
	}{
		"its service":     {[]string{"service:web"}, true},
		"another service": {[]string{"service:api"}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			deps := newExecDeps(&fakeTransport{node: "n1"})
			h := execRouter(t, deps, withACL(execPolicyOn(tc.resources, "exec")))

			w := do(t, h, http.MethodPost, "/tasks/t1/exec", "application/json")
			if got := w.Code == http.StatusCreated; got != tc.want {
				t.Errorf("POST = %d, want created: %v: %s", w.Code, tc.want, w.Body)
			}

			s := aliceSession(t, deps)
			var body struct{ Total int }
			_ = json.Unmarshal(
				do(t, h, http.MethodGet, "/exec", "application/json").Body.Bytes(),
				&body,
			)
			if want := map[bool]int{true: 2, false: 0}[tc.want]; body.Total != want {
				t.Errorf("/exec total = %d, want %d", body.Total, want)
			}

			hs := newTestHandlers(
				t,
				withCache(execCache()),
				withACL(execPolicyOn(tc.resources, "exec")),
			)
			if got := hs.ExecAuthorizer()(
				&auth.Identity{Subject: "alice"},
				s.Target,
			); got != tc.want {
				t.Errorf("ExecAuthorizer = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCreateExecSessionAnswersWithLocation(t *testing.T) {
	h := execRouter(t, newExecDeps(&fakeTransport{node: "n1", stream: newFakeStream()}))
	w := do(t, h, http.MethodPost, "/tasks/t1/exec", "application/json")
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "/exec/") {
		t.Fatalf("Location = %q", loc)
	}

	detail := do(t, h, http.MethodGet, loc, "application/json")
	var body map[string]any
	_ = json.Unmarshal(detail.Body.Bytes(), &body)
	if detail.Code != http.StatusOK || body["@id"] != loc || body["state"] != "pending" {
		t.Errorf("detail = %d %v", detail.Code, body)
	}
	if got := detail.Header().Get("Allow"); got != "GET, HEAD, DELETE" {
		t.Errorf("detail Allow = %q", got)
	}
}

func TestCreateExecSessionRefusals(t *testing.T) {
	cases := map[string]struct {
		opts []testHandlersOption
		deps ExecDeps
		path string
		code string
	}{
		"no exec grant": {
			[]testHandlersOption{withACL(execPolicy("read", "write"))},
			newExecDeps(&fakeTransport{node: "n1"}), "/tasks/t1/exec", "ACL003",
		},
		"level": {
			[]testHandlersOption{withOpsLevel(config.OpsConfiguration)},
			newExecDeps(&fakeTransport{node: "n1"}), "/tasks/t1/exec", "OPS001",
		},
		"not eligible": {nil, newExecDeps(&fakeTransport{node: "n1"}), "/tasks/t2/exec", "EXE001"},
		"no transport": {
			nil,
			newExecDeps(&fakeTransport{node: "elsewhere"}),
			"/tasks/t1/exec",
			"EXE002",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := do(
				t,
				execRouter(t, tc.deps, tc.opts...),
				http.MethodPost,
				tc.path,
				"application/json",
			)
			assertACLErrorCode(t, w, tc.code)
			if live := tc.deps.Manager.List(); len(live) != 0 {
				t.Errorf("a refused request left %d sessions", len(live))
			}
		})
	}
}

func TestExecTokenCeilingNarrowsTheTier(t *testing.T) {
	h := newTestRouterWithConfig(t, tokenRouterOptions(t),
		withCache(execCache()), withOpsLevel(config.OpsImpactful),
		withTokenOpsLevel(config.OpsConfiguration),
		withACL(execPolicy("read", "exec")), withExec(newExecDeps(&fakeTransport{node: "n1"})))
	bearer := tokenFor(t, "", &auth.Identity{Subject: "bob"})

	send := func(method string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/tasks/t1/exec", nil)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}

	if w := send(
		http.MethodGet,
	); w.Code != http.StatusOK ||
		strings.Contains(w.Header().Get("Allow"), "POST") {
		t.Errorf("GET = %d, Allow = %q", w.Code, w.Header().Get("Allow"))
	}
	assertACLErrorCode(t, send(http.MethodPost), "OPS001")
}

// recordingAudit keeps every record it is handed.
type recordingAudit struct {
	mu      sync.Mutex
	records []audit.Record
}

func (a *recordingAudit) Write(rec audit.Record) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, rec)
	return nil
}

func (a *recordingAudit) denials() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var reasons []string
	for _, rec := range a.records {
		if rec.Event == "exec.denied" {
			reasons = append(reasons, rec.Reason)
		}
	}
	return reasons
}

func TestCreateExecSessionCapacityIs429(t *testing.T) {
	cases := map[string]func(t *testing.T, m *exec.Manager, h http.Handler){
		"per identity": func(t *testing.T, _ *exec.Manager, h http.Handler) {
			do(t, h, http.MethodPost, "/tasks/t1/exec", "application/json")
			do(t, h, http.MethodPost, "/tasks/t1/exec", "application/json")
		},
		"instance": func(t *testing.T, m *exec.Manager, _ http.Handler) {
			for _, subject := range []string{"a", "b", "c", "d"} {
				id := &auth.Identity{Subject: subject}
				if _, err := m.Create(id, "", exec.Target{}, "local"); err != nil {
					t.Fatalf("Create: %v", err)
				}
			}
		},
	}
	for name, fill := range cases {
		t.Run(name, func(t *testing.T) {
			recorder := &recordingAudit{}
			deps := newExecDeps(&fakeTransport{node: "n1"})
			deps.Manager = exec.NewManager(exec.ManagerConfig{Audit: recorder, Limits: exec.Limits{
				MaxSessions: 4, MaxPerIdentity: 2, IdleTimeout: time.Hour, MaxDuration: time.Hour,
			}})
			h := execRouter(t, deps)
			fill(t, deps.Manager, h)

			w := do(t, h, http.MethodPost, "/tasks/t1/exec", "application/json")
			if w.Code != http.StatusTooManyRequests {
				t.Fatalf("status = %d: %s", w.Code, w.Body)
			}
			retry, err := strconv.Atoi(w.Header().Get("Retry-After"))
			if err != nil || retry < sse.RetryAfterFloor ||
				retry >= sse.RetryAfterFloor+sse.RetryAfterWindow {
				t.Errorf("Retry-After = %q", w.Header().Get("Retry-After"))
			}
			assertACLErrorCode(t, w, "EXE003")
			if got := recorder.denials(); !slices.Equal(got, []string{"EXE003"}) {
				t.Errorf("denials = %v, want [EXE003]", got)
			}
		})
	}
}

// A create changes state, so no precondition may turn it into a 304.
func TestCreateExecSessionNeverAnswers304(t *testing.T) {
	h := execRouter(t, newExecDeps(&fakeTransport{node: "n1"}))
	req := httptest.NewRequest(http.MethodPost, "/tasks/t1/exec", nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("If-None-Match", "*")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"state":"pending"`) {
		t.Errorf("status = %d, body = %s", w.Code, w.Body)
	}
}

func TestExecRoutesNegotiate(t *testing.T) {
	h := execRouter(t, newExecDeps(&fakeTransport{node: "n1"}))
	created := do(t, h, http.MethodPost, "/tasks/t1/exec", "application/json")
	loc := created.Header().Get("Location")

	for _, path := range []string{"/exec", loc, "/tasks/t1/exec"} {
		if w := do(t, h, http.MethodGet, path, "text/html"); w.Code != http.StatusOK ||
			!strings.Contains(w.Body.String(), "<html>") {
			t.Errorf("%s as HTML = %d", path, w.Code)
		}
		for _, accept := range []string{"application/atom+xml", "text/csv", "text/event-stream"} {
			if w := do(t, h, http.MethodGet, path, accept); w.Code != http.StatusNotAcceptable {
				t.Errorf("%s as %s = %d, want 406", path, accept, w.Code)
			}
		}
	}

	list := do(t, h, http.MethodGet, "/exec", "application/json")
	var body struct{ Total int }
	_ = json.Unmarshal(list.Body.Bytes(), &body)
	if body.Total != 1 {
		t.Errorf("/exec total = %d", body.Total)
	}
	if got := list.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("/exec Allow = %q", got)
	}
}

func TestDeleteExecSessionEndsIt(t *testing.T) {
	deps := newExecDeps(&fakeTransport{node: "n1"})
	h := execRouter(t, deps)
	loc := do(t, h, http.MethodPost, "/tasks/t1/exec", "application/json").Header().Get("Location")

	if w := do(t, h, http.MethodDelete, loc, ""); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d: %s", w.Code, w.Body)
	}
	s, _ := deps.Manager.Get(strings.TrimPrefix(loc, "/exec/"))
	if s.State != exec.StateEnded || s.Reason != "terminated_by" || s.EndedBy != "anonymous" {
		t.Errorf("session = %+v", s)
	}
	ended := do(t, h, http.MethodGet, loc, "application/json")
	if got := ended.Header().Get("Allow"); got != "GET, HEAD, DELETE" {
		t.Errorf("ended session Allow = %q", got)
	}
	if w := do(t, h, http.MethodDelete, loc, ""); w.Code != http.StatusNoContent {
		t.Errorf("repeated DELETE = %d", w.Code)
	}
}

func TestExecRoutesAbsentWhenDisabled(t *testing.T) {
	h := newTestRouterWithConfig(t, nil, withCache(execCache()), withOpsLevel(config.OpsImpactful))
	if w := do(t, h, http.MethodGet, "/exec", "application/json"); w.Code != http.StatusNotFound {
		t.Errorf("/exec = %d, want 404", w.Code)
	}
	if w := do(
		t,
		h,
		http.MethodPost,
		"/tasks/t1/exec",
		"application/json",
	); w.Code == http.StatusCreated {
		t.Error("POST /tasks/t1/exec created a session with exec disabled")
	}
}

func TestOthersSeeSessionsOnlyWithExecOnTheTarget(t *testing.T) {
	deps := newExecDeps(&fakeTransport{node: "n1"})
	s := aliceSession(t, deps)

	readOnly := execRouter(t, deps, withACL(execPolicy("read")))
	var body struct{ Total int }
	_ = json.Unmarshal(
		do(t, readOnly, http.MethodGet, "/exec", "application/json").Body.Bytes(),
		&body,
	)
	if body.Total != 0 {
		t.Errorf("a reader without exec saw %d sessions", body.Total)
	}
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		if w := do(
			t,
			readOnly,
			method,
			"/exec/"+s.ID,
			"application/json",
		); w.Code != http.StatusNotFound {
			t.Errorf("%s someone else's session = %d, want 404", method, w.Code)
		}
	}
	if got, _ := deps.Manager.Get(s.ID); got.State == exec.StateEnded {
		t.Error("a reader without exec ended someone else's session")
	}

	operator := execRouter(t, deps)
	if w := do(t, operator, http.MethodDelete, "/exec/"+s.ID, ""); w.Code != http.StatusNoContent {
		t.Errorf("DELETE with exec on the target = %d", w.Code)
	}
}

func TestOwnersSeeTheirSessionsAfterLosingExec(t *testing.T) {
	deps := newExecDeps(&fakeTransport{node: "n1"})
	loc := do(t, execRouter(t, deps), http.MethodPost, "/tasks/t1/exec", "application/json").
		Header().Get("Location")

	revoked := execRouter(t, deps, withACL(execPolicy("read")))
	if w := do(t, revoked, http.MethodGet, loc, "application/json"); w.Code != http.StatusOK {
		t.Errorf("owner reading their session = %d", w.Code)
	}
}

func TestExecOriginsAreTheTrustedOrigins(t *testing.T) {
	cfg := testRouterConfig(t, []routerOption{withCORS("https://a.example")})
	cfg.PublicURL = "https://cetacean.example"
	NewRouter(cfg)

	want := []string{"https://a.example", "https://cetacean.example"}
	if !slices.Equal(cfg.Handlers.execOrigins, want) {
		t.Errorf("execOrigins = %v, want %v", cfg.Handlers.execOrigins, want)
	}
}

func TestSetExecGivesALocalTransportTheCachedNodeID(t *testing.T) {
	var lookups int
	h := newTestHandlers(t, withSystemClient(&mockSystemClient{
		localNodeIDFn: func(context.Context) (string, error) {
			lookups++
			return "n1", nil
		},
	}))
	local := &exec.Local{}
	h.SetExec(ExecDeps{Transports: []exec.Transport{local}})

	if !local.Reaches("n1") || local.Reaches("n2") || !local.Reaches("n1") {
		t.Error("the local transport does not recognise the daemon's node")
	}
	if lookups != 1 {
		t.Errorf("node ID looked up %d times, want once", lookups)
	}
}
