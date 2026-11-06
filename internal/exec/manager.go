package exec

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/radiergummi/cetacean/internal/audit"
	"github.com/radiergummi/cetacean/internal/auth"
)

const (
	AttachWindow = 60 * time.Second
	Retention    = 5 * time.Minute
)

var (
	ErrCapacity         = errors.New("exec session capacity reached")
	ErrIdentityCapacity = errors.New("exec session capacity for this identity reached")
	ErrNotFound         = errors.New("exec session not found")
	ErrNotOwner         = errors.New("exec session belongs to someone else")
	ErrAlreadyAttached  = errors.New("exec session is already attached or has ended")
	ErrNotPending       = errors.New("exec session is not attached and waiting to start")
)

type Target struct {
	Task        string `json:"task"`
	Service     string `json:"service"`
	ServiceName string `json:"serviceName"`
	Stack       string `json:"stack,omitempty"`
	Node        string `json:"node"`
	Container   string `json:"container"`
}

type Owner struct {
	Subject     string   `json:"subject"`
	Provider    string   `json:"provider"`
	DisplayName string   `json:"displayName,omitempty"`
	Groups      []string `json:"-"`
}

func OwnerOf(id *auth.Identity) Owner {
	if id == nil {
		return Owner{}
	}

	return Owner{
		Subject: id.Subject, Provider: id.Provider,
		DisplayName: id.DisplayName, Groups: slices.Clone(id.Groups),
	}
}

func (o Owner) auditIdentity() audit.Identity {
	return audit.Identity{Subject: o.Subject, Provider: o.Provider, Groups: o.Groups}
}

func (o Owner) Is(id *auth.Identity) bool {
	return id != nil && o.Subject == id.Subject && o.Provider == id.Provider
}

type State string

const (
	StatePending   State = "pending"
	StateConnected State = "connected"
	StateEnded     State = "ended"
)

// Session is a snapshot; the manager's own copy changes under it.
type Session struct {
	ID          string     `json:"id"`
	Owner       Owner      `json:"owner"`
	Target      Target     `json:"target"`
	Transport   string     `json:"transport"`
	Client      string     `json:"-"`
	State       State      `json:"state"`
	CreatedAt   time.Time  `json:"createdAt"`
	ConnectedAt *time.Time `json:"connectedAt,omitempty"`
	EndedAt     *time.Time `json:"endedAt,omitempty"`
	Reason      string     `json:"reason,omitempty"`
	EndedBy     *Owner     `json:"endedBy,omitempty"`
	Exit        *Exit      `json:"exit,omitempty"`
	BytesIn     int64      `json:"bytesIn"`
	BytesOut    int64      `json:"bytesOut"`
}

type Limits struct {
	MaxSessions    int
	MaxPerIdentity int
	IdleTimeout    time.Duration
	MaxDuration    time.Duration
}

type ManagerConfig struct {
	Limits Limits
	Audit  audit.Logger
	Now    func() time.Time

	// PolicyGeneration and Authorize re-check live sessions when the ACL
	// policy is replaced; either nil disables revocation.
	PolicyGeneration func() uint64
	Authorize        func(*auth.Identity, Target) bool
}

type entry struct {
	Session
	identity  *auth.Identity
	lastInput time.Time
	attached  bool
	cancel    context.CancelFunc

	// starting holds the end record back from Finish: Connected writes it, so
	// that it can never land before the start record.
	starting bool
}

type Manager struct {
	cfg ManagerConfig

	mu             sync.Mutex
	sessions       map[string]*entry
	lastGeneration uint64

	// endWrites counts end records being written after their session ended,
	// which Shutdown waits for as it does for the sessions themselves.
	endWrites int
}

func NewManager(cfg ManagerConfig) *Manager {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Audit == nil {
		cfg.Audit = audit.Nop{}
	}

	m := &Manager{cfg: cfg, sessions: make(map[string]*entry)}
	if cfg.PolicyGeneration != nil {
		m.lastGeneration = cfg.PolicyGeneration()
	}

	return m
}

func newID() string {
	b := make([]byte, 10)
	_, _ = rand.Read(b)

	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}

// Create records the session. Unlike a start, an open that cannot be recorded
// still happens: nothing runs until Connected, which fails closed.
func (m *Manager) Create(
	id *auth.Identity,
	client string,
	target Target,
	transport string,
) (Session, error) {
	m.mu.Lock()

	owner := OwnerOf(id)
	live, mine := 0, 0
	for _, e := range m.sessions {
		if e.State == StateEnded {
			continue
		}
		live++
		if e.Owner.Is(id) {
			mine++
		}
	}
	if mine >= m.cfg.Limits.MaxPerIdentity {
		m.mu.Unlock()
		return Session{}, ErrIdentityCapacity
	}
	if live >= m.cfg.Limits.MaxSessions {
		m.mu.Unlock()
		return Session{}, ErrCapacity
	}

	e := &entry{
		ID: newID(), Owner: owner, Target: target, Transport: transport,
		Client: client, State: StatePending, CreatedAt: m.cfg.Now(),
		identity: id,
	}
	m.sessions[e.ID] = e
	rec := m.record("exec.open", e)
	session := e.Session
	m.mu.Unlock()

	_ = m.write(rec)

	return session, nil
}

func (m *Manager) Get(sid string) (Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.sessions[sid]
	if !ok {
		return Session{}, false
	}

	return e.Session, true
}

// List returns every session, newest first; visibility is the caller's rule.
func (m *Manager) List() []Session {
	m.mu.Lock()
	out := make([]Session, 0, len(m.sessions))
	for _, e := range m.sessions {
		out = append(out, e.Session)
	}
	m.mu.Unlock()

	slices.SortFunc(out, func(a, b Session) int { return b.CreatedAt.Compare(a.CreatedAt) })

	return out
}

// Attach claims the session for its owner, once. The context is cancelled when
// the session is ended, by whoever ends it.
func (m *Manager) Attach(sid string, id *auth.Identity) (context.Context, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.sessions[sid]
	switch {
	case !ok:
		return nil, ErrNotFound
	case !e.Owner.Is(id):
		return nil, ErrNotOwner
	case e.attached || e.State == StateEnded:
		return nil, ErrAlreadyAttached
	}

	ctx, cancel := context.WithCancel(context.Background())
	e.attached, e.cancel = true, cancel

	return ctx, nil
}

// Connected records the start; a start that cannot be audited does not happen.
// ErrNotPending means the shell must not run: the session was never attached,
// or was ended before or while its start was recorded.
func (m *Manager) Connected(sid string) error {
	m.mu.Lock()
	e, ok := m.sessions[sid]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	if !e.attached || e.State != StatePending || e.Reason != "" {
		m.mu.Unlock()
		return ErrNotPending
	}
	now := m.cfg.Now()
	e.starting = true
	rec := m.record("exec.start", e)
	m.mu.Unlock()

	if err := m.write(rec); err != nil {
		var landed <-chan error
		if abandoned, ok := errors.AsType[*audit.AbandonedError](err); ok {
			landed = abandoned.Landed
		}
		m.abortLate(sid, landed)

		return audit.ErrUnavailable
	}

	// A start record that was written must be paired with an end record.
	m.mu.Lock()
	e.starting = false
	switch {
	case e.State == StatePending && e.Reason == "":
		e.State, e.ConnectedAt, e.lastInput = StateConnected, &now, now
		m.mu.Unlock()

		return nil
	case e.State == StatePending:
		e.State, e.ConnectedAt, e.lastInput = StateConnected, &now, now
		m.mu.Unlock()
		m.Finish(sid, Exit{Code: -1, Outcome: OutcomeDetached})
	default:
		end := m.record("exec.end", e)
		m.mu.Unlock()
		_ = m.write(end)
	}

	return ErrNotPending
}

func (m *Manager) Input(sid string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.sessions[sid]; ok {
		e.BytesIn += int64(n)
		e.lastInput = m.cfg.Now()
	}
}

func (m *Manager) Output(sid string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.sessions[sid]; ok {
		e.BytesOut += int64(n)
	}
}

// End asks a session to stop; the first reason given is the one recorded. An
// attached session finishes through its pump, an unattached one right here.
func (m *Manager) End(sid, reason string, by *auth.Identity) {
	m.mu.Lock()
	e, ok := m.sessions[sid]
	if !ok || e.State == StateEnded || e.Reason != "" {
		m.mu.Unlock()
		return
	}
	e.Reason = reason
	if by != nil {
		e.EndedBy = new(OwnerOf(by))
	}
	cancel, attached := e.cancel, e.attached
	m.mu.Unlock()

	if attached {
		cancel()
		return
	}

	m.Finish(sid, Exit{Code: -1, Outcome: OutcomeDetached})
}

// Abort ends a session whose shell never ran, so it can never read as exited.
// A reason already given still wins.
func (m *Manager) Abort(sid, reason string) {
	m.mu.Lock()
	if e, ok := m.sessions[sid]; ok && e.State != StateEnded && e.Reason == "" {
		e.Reason = reason
	}
	m.mu.Unlock()

	m.Finish(sid, Exit{Code: -1, Outcome: OutcomeDetached})
}

// Finish marks the session ended, once, and writes the end record.
func (m *Manager) Finish(sid string, exit Exit) Session {
	m.mu.Lock()
	e, ok := m.sessions[sid]
	if !ok {
		m.mu.Unlock()
		return Session{}
	}
	if e.State == StateEnded {
		snapshot := e.Session
		m.mu.Unlock()
		return snapshot
	}

	writeEnd := !e.starting
	now := m.cfg.Now()
	e.State, e.EndedAt, e.Exit = StateEnded, &now, &exit
	// A shell that exited on its own did, whatever asked it to stop meanwhile.
	switch {
	case exit.Outcome == OutcomeExited:
		e.Reason, e.EndedBy = "exited", nil
	case e.Reason == "":
		e.Reason = "exited"
	}
	if e.cancel != nil {
		e.cancel()
	}
	rec := m.record("exec.end", e)
	snapshot := e.Session
	if writeEnd {
		m.endWrites++
	}
	m.mu.Unlock()

	if writeEnd {
		_ = m.write(rec)

		m.mu.Lock()
		m.endWrites--
		m.mu.Unlock()
	}

	return snapshot
}

// abortLate aborts a session whose start record failed or, given up on, may
// still land, which landed then reports; the end record waits for it. The write
// is counted before the abort, so Shutdown always sees it.
func (m *Manager) abortLate(sid string, landed <-chan error) {
	m.mu.Lock()
	m.endWrites++
	m.mu.Unlock()

	m.Abort(sid, "audit_unavailable")

	m.mu.Lock()
	end := m.record("exec.end", m.sessions[sid])
	m.mu.Unlock()

	go func() {
		if landed != nil {
			<-landed
		}
		_ = m.write(end)

		m.mu.Lock()
		m.endWrites--
		m.mu.Unlock()
	}()
}

// Denied records a refusal that never became a session.
func (m *Manager) Denied(id *auth.Identity, client string, target Target, reason string) {
	rec := m.record("exec.denied", &entry{
		Owner: OwnerOf(id), Target: target, Client: client, Reason: reason, identity: id,
	})
	_ = m.write(rec)
}

// DeniedSession records a refused attach to or end of the session sid, if it
// exists, as the caller's refusal.
func (m *Manager) DeniedSession(sid string, id *auth.Identity, client, reason string) {
	m.mu.Lock()
	e, ok := m.sessions[sid]
	if !ok {
		m.mu.Unlock()
		return
	}
	rec := m.record("exec.denied", &entry{
		ID: sid, Owner: OwnerOf(id), Target: e.Target, Transport: e.Transport,
		Client: client, Reason: reason,
	})
	m.mu.Unlock()

	_ = m.write(rec)
}

// write fails closed: a record that cannot be written ends every connected
// session too, since none of them can be audited to its end. End only cancels
// an attached session, so this never re-enters write.
func (m *Manager) write(rec audit.Record) error {
	err := m.cfg.Audit.Write(rec)
	if err == nil {
		return nil
	}
	slog.Error("exec audit record not written",
		"event", rec.Event, "session", rec.Session, "error", err)

	m.mu.Lock()
	var running []string
	for id, e := range m.sessions {
		if e.State == StateConnected {
			running = append(running, id)
		}
	}
	m.mu.Unlock()

	for _, id := range running {
		m.End(id, "audit_unavailable", nil)
	}

	return err
}

// record must be called with m.mu held or on an entry nobody else holds.
func (m *Manager) record(event string, e *entry) audit.Record {
	rec := audit.Record{
		Time: m.cfg.Now(), Event: event, Session: e.ID, Identity: e.Owner.auditIdentity(),
		Client: e.Client, Transport: e.Transport,
		Target: &audit.Target{
			Task: e.Target.Task, Service: e.Target.ServiceName, Stack: e.Target.Stack,
			Node: e.Target.Node, Container: e.Target.Container,
		},
		Reason: e.Reason,
	}
	if e.EndedBy != nil {
		rec.EndedBy = new(e.EndedBy.auditIdentity())
	}
	if event == "exec.end" {
		rec.BytesIn, rec.BytesOut = e.BytesIn, e.BytesOut
		if e.Exit != nil {
			rec.Outcome, rec.ExitCode = string(e.Exit.Outcome), new(e.Exit.Code)
		}
		if e.ConnectedAt != nil && e.EndedAt != nil {
			rec.Duration = e.EndedAt.Sub(*e.ConnectedAt).String()
		}
	}

	return rec
}

// Sweep applies every time-based rule and revocation; Run calls it each second.
func (m *Manager) Sweep() {
	now := m.cfg.Now()

	var generation uint64
	if m.cfg.PolicyGeneration != nil && m.cfg.Authorize != nil {
		generation = m.cfg.PolicyGeneration()
	}

	type ending struct{ id, reason string }
	var endings []ending
	type check struct {
		id       string
		identity *auth.Identity
		target   Target
	}
	var checks []check

	m.mu.Lock()
	revoke := m.cfg.Authorize != nil && generation != m.lastGeneration
	if revoke {
		m.lastGeneration = generation
	}
	for id, e := range m.sessions {
		switch {
		case e.State == StateEnded:
			if e.EndedAt != nil && now.Sub(*e.EndedAt) > Retention {
				delete(m.sessions, id)
			}
		case e.State == StatePending && !e.attached && now.Sub(e.CreatedAt) > AttachWindow:
			endings = append(endings, ending{id, "not_attached"})
		case e.State == StateConnected && now.Sub(*e.ConnectedAt) > m.cfg.Limits.MaxDuration:
			endings = append(endings, ending{id, "max_duration"})
		case e.State == StateConnected && now.Sub(e.lastInput) > m.cfg.Limits.IdleTimeout:
			endings = append(endings, ending{id, "idle_timeout"})
		case revoke:
			checks = append(checks, check{id, e.identity, e.Target})
		}
	}
	m.mu.Unlock()

	for _, c := range checks {
		if !m.cfg.Authorize(c.identity, c.target) {
			endings = append(endings, ending{c.id, "revoked"})
		}
	}
	for _, end := range endings {
		m.End(end.id, end.reason, nil)
	}
}

// Shutdown ends every session with reason "shutdown" and waits, until ctx
// expires, for the pumps of those already attached to finish them and for
// their end records to be written.
func (m *Manager) Shutdown(ctx context.Context) {
	for _, s := range m.List() {
		m.End(s.ID, "shutdown", nil)
	}

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for m.settling() {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// settling reports whether an attached session is still live or an end record
// is still being written.
func (m *Manager) settling() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.endWrites > 0 {
		return true
	}

	for _, e := range m.sessions {
		if e.attached && e.State != StateEnded {
			return true
		}
	}

	return false
}

func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Sweep()
		}
	}
}
