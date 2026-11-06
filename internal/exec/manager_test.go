package exec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/audit"
	"github.com/radiergummi/cetacean/internal/auth"
)

type recorder struct {
	mu      sync.Mutex
	records []audit.Record
	fail    bool
	onWrite func(audit.Record)
	refuse  func(audit.Record) error
}

func (r *recorder) Write(rec audit.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return audit.ErrUnavailable
	}
	if r.refuse != nil {
		if err := r.refuse(rec); err != nil {
			return err
		}
	}
	if r.onWrite != nil {
		r.onWrite(rec)
	}
	r.records = append(r.records, rec)
	return nil
}

func (r *recorder) events() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, rec := range r.records {
		out = append(out, rec.Event)
	}
	return out
}

func assertEvents(t *testing.T, rec *recorder, want ...string) {
	t.Helper()
	if got := rec.events(); !slices.Equal(got, want) {
		t.Errorf("audit events = %v, want %v", got, want)
	}
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time          { return c.now }
func (c *clock) advance(d time.Duration) { c.now = c.now.Add(d) }

var (
	alice  = &auth.Identity{Subject: "alice", Provider: "oidc"}
	bob    = &auth.Identity{Subject: "bob", Provider: "oidc"}
	target = Target{Task: "t1", Service: "s1", ServiceName: "web", Node: "n1", Container: "c1"}
)

func newTestManager(t *testing.T, limits Limits) (*Manager, *recorder, *clock) {
	t.Helper()
	rec, clk := &recorder{}, &clock{now: time.Unix(1_700_000_000, 0)}
	if limits.MaxSessions == 0 {
		limits.MaxSessions = 4
	}
	if limits.MaxPerIdentity == 0 {
		limits.MaxPerIdentity = 2
	}
	if limits.IdleTimeout == 0 {
		limits.IdleTimeout = 30 * time.Minute
	}
	if limits.MaxDuration == 0 {
		limits.MaxDuration = 4 * time.Hour
	}
	m := NewManager(ManagerConfig{Limits: limits, Audit: rec, Now: clk.Now})
	return m, rec, clk
}

func connect(t *testing.T, m *Manager, id *auth.Identity) Session {
	t.Helper()
	s, err := m.Create(id, "192.0.2.1", target, "local")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := m.Attach(s.ID, id); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := m.Connected(s.ID); err != nil {
		t.Fatalf("connected: %v", err)
	}
	return s
}

func TestManagerCaps(t *testing.T) {
	m, _, _ := newTestManager(t, Limits{MaxSessions: 3, MaxPerIdentity: 2})
	connect(t, m, alice)
	connect(t, m, alice)
	if _, err := m.Create(alice, "", target, "local"); !errors.Is(err, ErrIdentityCapacity) {
		t.Errorf("third for alice = %v, want ErrIdentityCapacity", err)
	}
	connect(t, m, bob)
	if _, err := m.Create(bob, "", target, "local"); !errors.Is(err, ErrCapacity) {
		t.Errorf("fourth overall = %v, want ErrCapacity", err)
	}
}

func TestManagerAttachOwnerOnly(t *testing.T) {
	m, _, _ := newTestManager(t, Limits{})
	s, _ := m.Create(alice, "", target, "local")
	if _, err := m.Attach(s.ID, bob); !errors.Is(err, ErrNotOwner) {
		t.Errorf("bob attaching = %v, want ErrNotOwner", err)
	}
	if _, err := m.Attach("nope", alice); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown = %v, want ErrNotFound", err)
	}
}

func TestManagerAttachIsExclusiveUnderRace(t *testing.T) {
	m, _, _ := newTestManager(t, Limits{})
	s, _ := m.Create(alice, "", target, "local")

	var won, refused atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if _, err := m.Attach(s.ID, alice); err == nil {
				won.Add(1)
			} else if errors.Is(err, ErrAlreadyAttached) {
				refused.Add(1)
			}
		})
	}
	wg.Wait()
	if won.Load() != 1 || refused.Load() != 15 {
		t.Errorf("won=%d refused=%d, want 1 and 15", won.Load(), refused.Load())
	}
}

func TestManagerFailsClosedOnStartRecord(t *testing.T) {
	m, rec, _ := newTestManager(t, Limits{})
	rec.fail = true
	s, _ := m.Create(alice, "", target, "local")
	ctx, _ := m.Attach(s.ID, alice)
	if err := m.Connected(s.ID); !errors.Is(err, audit.ErrUnavailable) {
		t.Fatalf("connected = %v, want audit.ErrUnavailable", err)
	}
	got, _ := m.Get(s.ID)
	if got.State != StateEnded || got.Reason != "audit_unavailable" ||
		got.Exit == nil || *got.Exit != (Exit{Code: -1, Outcome: OutcomeDetached}) {
		t.Errorf("session = %+v", got)
	}
	if ctx.Err() == nil {
		t.Error("a refused session kept its attach context")
	}
}

func TestManagerPairsAnAbandonedStartThatLands(t *testing.T) {
	for _, tc := range []struct {
		name   string
		landed error
		want   []string
	}{
		{"landed", nil, []string{"exec.open", "exec.end"}},
		{"never landed", errors.New("disk full"), []string{"exec.open", "exec.end"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, rec, _ := newTestManager(t, Limits{})
			landed := make(chan error, 1)
			rec.refuse = func(r audit.Record) error {
				if r.Event == "exec.start" {
					return &audit.AbandonedError{Landed: landed}
				}
				return nil
			}
			s, _ := m.Create(alice, "", target, "local")
			_, _ = m.Attach(s.ID, alice)
			if err := m.Connected(s.ID); !errors.Is(err, audit.ErrUnavailable) {
				t.Fatalf("connected = %v, want audit.ErrUnavailable", err)
			}
			if events := rec.events(); !slices.Equal(events, []string{"exec.open"}) {
				t.Fatalf("wrote %v before the start landed", events)
			}

			landed <- tc.landed
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			m.Shutdown(ctx)

			got := rec.events()
			if !slices.Equal(got, tc.want) {
				t.Fatalf("events after the start ended = %v, want %v", got, tc.want)
			}
			end := rec.records[len(rec.records)-1]
			if end.Session != s.ID || end.Reason != "audit_unavailable" ||
				end.Outcome != string(OutcomeDetached) {
				t.Errorf("end record = %+v", end)
			}
		})
	}
}

func TestManagerStartRecordFailureEndsRunningSessions(t *testing.T) {
	m, rec, _ := newTestManager(t, Limits{})
	a, b := connect(t, m, alice), connect(t, m, bob)

	rec.fail = true
	c, _ := m.Create(alice, "", target, "local")
	_, _ = m.Attach(c.ID, alice)
	if err := m.Connected(c.ID); !errors.Is(err, audit.ErrUnavailable) {
		t.Fatalf("connected = %v, want audit.ErrUnavailable", err)
	}

	if got, _ := m.Get(c.ID); got.State != StateEnded || got.Reason != "audit_unavailable" {
		t.Errorf("refused session = %+v", got)
	}
	for _, s := range []Session{a, b} {
		if got, _ := m.Get(s.ID); got.Reason != "audit_unavailable" {
			t.Errorf("running session %s = %+v", s.ID, got)
		}
		// The pump finishes it; its end record fails too and must not loop.
		got := m.Finish(s.ID, Exit{Code: 129, Outcome: OutcomeTerminated})
		if got.State != StateEnded || got.Reason != "audit_unavailable" {
			t.Errorf("finished session %s = %+v", s.ID, got)
		}
	}
}

func TestManagerEndRecordFailureEndsRunningSessions(t *testing.T) {
	m, rec, _ := newTestManager(t, Limits{})
	a := connect(t, m, alice)
	b, _ := m.Create(bob, "", target, "local")
	ctxB, _ := m.Attach(b.ID, bob)
	_ = m.Connected(b.ID)

	rec.fail = true
	m.End(a.ID, "disconnected", nil)
	m.Finish(a.ID, Exit{Code: 129, Outcome: OutcomeTerminated})

	if got, _ := m.Get(b.ID); got.Reason != "audit_unavailable" || ctxB.Err() == nil {
		t.Errorf("other running session = %+v", got)
	}
	if got, _ := m.Get(a.ID); got.Reason != "disconnected" {
		t.Errorf("finished session = %+v", got)
	}
}

func TestManagerDeniedRecordFailureEndsRunningSessions(t *testing.T) {
	m, rec, _ := newTestManager(t, Limits{})
	a := connect(t, m, alice)

	rec.fail = true
	m.Denied(bob, "", target, "forbidden")

	if got, _ := m.Get(a.ID); got.Reason != "audit_unavailable" {
		t.Errorf("running session = %+v", got)
	}
}

func TestManagerAbortIsDetached(t *testing.T) {
	m, rec, _ := newTestManager(t, Limits{})
	s, _ := m.Create(alice, "", target, "local")
	ctx, _ := m.Attach(s.ID, alice)

	m.Abort(s.ID, "transport_unavailable")
	m.Abort(s.ID, "later")

	got, _ := m.Get(s.ID)
	if got.State != StateEnded || got.Reason != "transport_unavailable" ||
		got.Exit == nil || *got.Exit != (Exit{Code: -1, Outcome: OutcomeDetached}) {
		t.Errorf("session = %+v", got)
	}
	if ctx.Err() == nil {
		t.Error("Abort did not cancel the attach context")
	}
	assertEvents(t, rec, "exec.open", "exec.end")
	if end := rec.records[1]; end.Reason != "transport_unavailable" || end.Duration != "" ||
		end.Outcome != string(OutcomeDetached) {
		t.Errorf("end record = %+v", end)
	}
}

func TestManagerEndUnattachedIsDetached(t *testing.T) {
	m, rec, _ := newTestManager(t, Limits{})
	s, _ := m.Create(alice, "", target, "local")

	m.End(s.ID, "terminated_by", bob)

	got, _ := m.Get(s.ID)
	if got.State != StateEnded || got.EndedBy == nil || !got.EndedBy.Is(bob) ||
		got.Exit == nil || got.Exit.Outcome != OutcomeDetached || got.Exit.Code != -1 {
		t.Errorf("session = %+v", got)
	}
	if _, err := m.Attach(s.ID, alice); !errors.Is(err, ErrAlreadyAttached) {
		t.Errorf("attach after end = %v, want ErrAlreadyAttached", err)
	}
	assertEvents(t, rec, "exec.open", "exec.end")
	if end := rec.records[1]; end.EndedBy == nil || end.EndedBy.Subject != "bob" {
		t.Errorf("end record = %+v", end)
	}
}

func TestManagerRecordsTheOpen(t *testing.T) {
	m, rec, _ := newTestManager(t, Limits{})
	s, err := m.Create(alice, "192.0.2.1", target, "local")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if len(rec.records) != 1 {
		t.Fatalf("audit records = %+v", rec.records)
	}
	open := rec.records[0]
	if open.Event != "exec.open" || open.Session != s.ID || open.Identity.Subject != "alice" ||
		open.Client != "192.0.2.1" || open.Transport != "local" || open.Target.Task != "t1" {
		t.Errorf("open record = %+v", open)
	}
}

func TestManagerShowsNoSessionBeforeItsOpenIsWritten(t *testing.T) {
	m, rec, _ := newTestManager(t, Limits{})
	var seen []Session
	rec.onWrite = func(audit.Record) { seen = m.List() }

	if _, err := m.Create(alice, "", target, "local"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(seen) != 0 {
		t.Errorf("sessions visible while the open was written = %+v", seen)
	}
	if got := m.List(); len(got) != 1 {
		t.Errorf("sessions after the open = %+v", got)
	}
}

func TestManagerCreateAfterShutdownIsRefused(t *testing.T) {
	m, rec, _ := newTestManager(t, Limits{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	m.Shutdown(ctx)

	if _, err := m.Create(alice, "", target, "local"); !errors.Is(err, ErrCapacity) {
		t.Errorf("create = %v, want ErrCapacity", err)
	}
	if got := rec.events(); len(got) != 0 {
		t.Errorf("audit events = %v", got)
	}
}

func TestManagerShutdownWaitsForACreateInProgress(t *testing.T) {
	m, rec, _ := newTestManager(t, Limits{})
	writing, release := make(chan struct{}), make(chan struct{})
	rec.refuse = func(r audit.Record) error {
		if r.Event == "exec.open" {
			close(writing)
			<-release
		}
		return nil
	}
	created := make(chan Session, 1)
	go func() {
		s, _ := m.Create(alice, "", target, "local")
		created <- s
	}()
	<-writing

	shut := make(chan struct{})
	go func() {
		defer close(shut)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	}()
	close(release)
	s := <-created
	<-shut

	got, ok := m.Get(s.ID)
	if !ok || got.State != StateEnded || got.Reason != "shutdown" {
		t.Errorf("session = %+v", got)
	}
}

func TestManagerCreateOutlivesAnUnwrittenOpenButNotItsStart(t *testing.T) {
	m, rec, _ := newTestManager(t, Limits{})
	rec.fail = true
	s, err := m.Create(alice, "", target, "local")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := m.Attach(s.ID, alice); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := m.Connected(s.ID); !errors.Is(err, audit.ErrUnavailable) {
		t.Errorf("connected = %v, want audit.ErrUnavailable", err)
	}
}

func TestManagerDeniedSessionRecordsTheCaller(t *testing.T) {
	m, rec, _ := newTestManager(t, Limits{})
	s, _ := m.Create(alice, "", target, "local")

	m.DeniedSession(s.ID, bob, "192.0.2.2", "OPS001")
	m.DeniedSession("missing", bob, "192.0.2.2", "OPS001")

	if got := rec.events(); !slices.Equal(got, []string{"exec.open", "exec.denied"}) {
		t.Fatalf("audit events = %v", got)
	}
	denied := rec.records[1]
	if denied.Session != s.ID || denied.Identity.Subject != "bob" || denied.Reason != "OPS001" ||
		denied.Target.Task != "t1" {
		t.Errorf("denied record = %+v", denied)
	}
}

func TestManagerEndCancelsAndFinishRecordsOnce(t *testing.T) {
	m, rec, clk := newTestManager(t, Limits{})
	s, _ := m.Create(alice, "", target, "local")
	ctx, _ := m.Attach(s.ID, alice)
	_ = m.Connected(s.ID)
	m.Input(s.ID, 5)
	m.Output(s.ID, 7)
	clk.advance(time.Minute)

	m.End(s.ID, "terminated_by", bob)
	m.End(s.ID, "disconnected", nil)
	if ctx.Err() == nil {
		t.Fatal("End did not cancel the attach context")
	}

	got := m.Finish(s.ID, Exit{Code: 129, Outcome: OutcomeTerminated})
	m.Finish(s.ID, Exit{Code: 0, Outcome: OutcomeExited})

	if got.Reason != "terminated_by" || got.EndedBy == nil || !got.EndedBy.Is(bob) ||
		got.Exit.Outcome != OutcomeTerminated {
		t.Errorf("first End or first Finish did not win: %+v", got)
	}
	if events := rec.events(); !slices.Equal(
		events, []string{"exec.open", "exec.start", "exec.end"},
	) {
		t.Errorf("audit events = %v", events)
	}
	end := rec.records[2]
	if end.BytesIn != 5 || end.BytesOut != 7 || end.Duration != "1m0s" || *end.ExitCode != 129 {
		t.Errorf("end record = %+v", end)
	}
	if end.EndedBy == nil || end.EndedBy.Subject != "bob" || end.EndedBy.Provider != "oidc" {
		t.Errorf("end record endedBy = %+v, want bob from oidc", end.EndedBy)
	}
}

func TestManagerFinishRecordsAnExitOverAnEarlierReason(t *testing.T) {
	m, rec, _ := newTestManager(t, Limits{})
	s := connect(t, m, alice)

	m.End(s.ID, "terminated_by", bob)
	got := m.Finish(s.ID, Exit{Code: 7, Outcome: OutcomeExited})

	if got.Reason != "exited" || got.EndedBy != nil || got.Exit.Code != 7 {
		t.Errorf("session = %+v exit %+v, want reason exited and no endedBy", got, got.Exit)
	}
	end := rec.records[len(rec.records)-1]
	if end.Event != "exec.end" || end.Reason != "exited" || end.EndedBy != nil ||
		end.Outcome != "exited" {
		t.Errorf("end record = %+v", end)
	}
}

func TestManagerSweepIdleCountsInputOnly(t *testing.T) {
	m, _, clk := newTestManager(t, Limits{IdleTimeout: time.Minute})
	s := connect(t, m, alice)

	clk.advance(50 * time.Second)
	m.Output(s.ID, 100)
	clk.advance(20 * time.Second)
	m.Sweep()

	if got, _ := m.Get(s.ID); got.Reason != "idle_timeout" {
		t.Errorf("output kept the session alive: %+v", got)
	}
}

func TestManagerSweepMaxDuration(t *testing.T) {
	m, _, clk := newTestManager(t, Limits{MaxDuration: time.Hour})
	s := connect(t, m, alice)
	for range 7 {
		clk.advance(10 * time.Minute)
		m.Input(s.ID, 1)
	}
	m.Sweep()
	if got, _ := m.Get(s.ID); got.Reason != "max_duration" {
		t.Errorf("session = %+v", got)
	}
}

func TestManagerSweepEndsUnattachedAndForgetsOld(t *testing.T) {
	m, rec, clk := newTestManager(t, Limits{})
	s, _ := m.Create(alice, "", target, "local")
	clk.advance(AttachWindow + time.Second)
	m.Sweep()
	if got, _ := m.Get(s.ID); got.State != StateEnded || got.Reason != "not_attached" {
		t.Fatalf("session = %+v", got)
	}
	assertEvents(t, rec, "exec.open", "exec.end")

	clk.advance(Retention + time.Second)
	m.Sweep()
	if _, ok := m.Get(s.ID); ok {
		t.Error("an ended session outlived its retention")
	}
}

func TestManagerSweepRevokesOnPolicyChange(t *testing.T) {
	var generation atomic.Uint64
	allowed := atomic.Bool{}
	allowed.Store(true)

	rec, clk := &recorder{}, &clock{now: time.Unix(1_700_000_000, 0)}
	m := NewManager(ManagerConfig{
		Limits: Limits{
			MaxSessions:    4,
			MaxPerIdentity: 2,
			IdleTimeout:    time.Hour,
			MaxDuration:    time.Hour,
		},
		Audit:            rec,
		Now:              clk.Now,
		PolicyGeneration: generation.Load,
		Authorize:        func(*auth.Identity, Target) bool { return allowed.Load() },
	})
	s := connect(t, m, alice)

	m.Sweep()
	if got, _ := m.Get(s.ID); got.State != StateConnected {
		t.Fatalf("revoked without a policy change: %+v", got)
	}

	allowed.Store(false)
	generation.Add(1)
	m.Sweep()
	if got, _ := m.Get(s.ID); got.Reason != "revoked" {
		t.Errorf("session = %+v", got)
	}
}

func TestManagerConnectedRefusesWhatCannotStart(t *testing.T) {
	m, rec, _ := newTestManager(t, Limits{})
	unattached, _ := m.Create(alice, "", target, "local")
	if err := m.Connected(unattached.ID); !errors.Is(err, ErrNotPending) {
		t.Errorf("unattached = %v, want ErrNotPending", err)
	}

	ending, _ := m.Create(alice, "", target, "local")
	_, _ = m.Attach(ending.ID, alice)
	m.End(ending.ID, "terminated_by", bob)
	if err := m.Connected(ending.ID); !errors.Is(err, ErrNotPending) {
		t.Errorf("ending = %v, want ErrNotPending", err)
	}
	if slices.Contains(rec.events(), "exec.start") {
		t.Errorf("a refused start wrote %v", rec.events())
	}
}

func TestManagerConnectedPairsAStartThatLostARace(t *testing.T) {
	for name, interrupt := range map[string]func(*Manager, string){
		"ended":    func(m *Manager, sid string) { m.End(sid, "terminated_by", bob) },
		"finished": func(m *Manager, sid string) { m.Abort(sid, "transport_unavailable") },
	} {
		t.Run(name, func(t *testing.T) {
			m, rec, _ := newTestManager(t, Limits{})
			s, _ := m.Create(alice, "", target, "local")
			_, _ = m.Attach(s.ID, alice)
			rec.onWrite = func(r audit.Record) {
				if r.Event == "exec.start" {
					interrupt(m, s.ID)
				}
			}

			if err := m.Connected(s.ID); !errors.Is(err, ErrNotPending) {
				t.Fatalf("connected = %v, want ErrNotPending", err)
			}
			want := []string{"exec.open", "exec.start", "exec.end"}
			if events := rec.events(); !slices.Equal(events, want) {
				t.Errorf("audit events = %v, want %v", events, want)
			}
			got, _ := m.Get(s.ID)
			if got.State != StateEnded || got.Exit == nil || got.Exit.Outcome != OutcomeDetached {
				t.Errorf("session = %+v", got)
			}
		})
	}
}

func TestManagerSweepLeavesAStartingSessionToItsPump(t *testing.T) {
	m, _, clk := newTestManager(t, Limits{})
	s, _ := m.Create(alice, "", target, "local")
	_, _ = m.Attach(s.ID, alice)
	clk.advance(AttachWindow + time.Second)
	m.Sweep()
	if got, _ := m.Get(s.ID); got.State != StatePending || got.Reason != "" {
		t.Errorf("session = %+v", got)
	}
}

func TestManagerConcurrentFinishesKeepTheirReasons(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	file, err := audit.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })

	const n = 32
	m := NewManager(ManagerConfig{
		Limits: Limits{
			MaxSessions:    n,
			MaxPerIdentity: n,
			IdleTimeout:    time.Hour,
			MaxDuration:    time.Hour,
		},
		Audit: file,
	})
	sessions := make([]Session, n)
	for i := range sessions {
		sessions[i] = connect(t, m, alice)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, s := range sessions {
		wg.Go(func() {
			<-start
			m.End(s.ID, "disconnected", nil)
			m.Finish(s.ID, Exit{Code: 129, Outcome: OutcomeTerminated})
		})
	}
	close(start)
	wg.Wait()

	for _, s := range sessions {
		if got, _ := m.Get(s.ID); got.Reason != "disconnected" {
			t.Errorf("session %s = %+v", s.ID, got)
		}
	}
	b, _ := os.ReadFile(path)
	if got := strings.Count(string(b), `"exec.end"`); got != n {
		t.Errorf("%d end records on disk, want %d", got, n)
	}
}

func TestManagerShutdownEndsSessionsAndWaitsForTheirPumps(t *testing.T) {
	m, rec, _ := newTestManager(t, Limits{})
	running, _ := m.Create(alice, "", target, "local")
	ctx, _ := m.Attach(running.ID, alice)
	_ = m.Connected(running.ID)
	waiting, _ := m.Create(bob, "", target, "local")

	go func() {
		<-ctx.Done()
		time.Sleep(50 * time.Millisecond)
		m.Finish(running.ID, Exit{Code: 129, Outcome: OutcomeTerminated})
	}()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.Shutdown(shutdownCtx)

	if got, _ := m.Get(running.ID); got.State != StateEnded || got.Reason != "shutdown" ||
		got.Exit.Outcome != OutcomeTerminated {
		t.Errorf("running session = %+v exit %+v", got, got.Exit)
	}
	if got, _ := m.Get(waiting.ID); got.State != StateEnded || got.Reason != "shutdown" {
		t.Errorf("unattached session = %+v", got)
	}
	var ends []string
	for _, r := range rec.records {
		if r.Event == "exec.end" {
			ends = append(ends, r.Session)
			if r.Reason != "shutdown" {
				t.Errorf("end record = %+v", r)
			}
		}
	}
	slices.Sort(ends)
	want := []string{running.ID, waiting.ID}
	slices.Sort(want)
	if !slices.Equal(ends, want) {
		t.Errorf("sessions ended on record = %v, want %v", ends, want)
	}
}

func TestManagerShutdownWaitsForTheEndRecord(t *testing.T) {
	m, rec, _ := newTestManager(t, Limits{})
	s, _ := m.Create(alice, "", target, "local")
	ctx, _ := m.Attach(s.ID, alice)
	_ = m.Connected(s.ID)
	var written atomic.Bool
	rec.onWrite = func(r audit.Record) {
		if r.Event == "exec.end" {
			time.Sleep(50 * time.Millisecond)
			written.Store(true)
		}
	}

	go func() {
		<-ctx.Done()
		m.Finish(s.ID, Exit{Code: 129, Outcome: OutcomeTerminated})
	}()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.Shutdown(shutdownCtx)

	// Read without the recorder's lock, which a write in progress holds.
	if !written.Load() {
		t.Error("Shutdown returned before the session's end record was written")
	}
}

func TestManagerShutdownGivesUpWhenItsContextExpires(t *testing.T) {
	m, _, _ := newTestManager(t, Limits{})
	s := connect(t, m, alice)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		m.Shutdown(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown waited past its context for a pump that never finished")
	}
	if got, _ := m.Get(s.ID); got.Reason != "shutdown" || got.State != StateConnected {
		t.Errorf("session = %+v, want it asked to end and still awaiting its pump", got)
	}
}
