package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/api/problem"
	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/exec"
)

type ExecDeps struct {
	Manager    *exec.Manager
	Transports []exec.Transport
	Self       func(swarm.Task) bool

	// startTimeout bounds opening the shell; zero is exec.AttachWindow.
	startTimeout time.Duration
}

// SetExec enables the exec routes; NewRouter registers them only when it has run.
// A local transport given no NodeID shares the handlers' cached local node ID.
func (h *Handlers) SetExec(deps ExecDeps) {
	for _, transport := range deps.Transports {
		if local, ok := transport.(*exec.Local); ok && local.NodeID == nil {
			local.NodeID = func() string { return h.getLocalNodeID(context.Background()) }
		}
	}
	h.exec = &deps
}

// execRefusal is a check that failed, as the problem the caller is shown.
type execRefusal struct{ code, detail string }

// execTarget resolves a task to what a session records about it.
func (h *Handlers) execTarget(task swarm.Task) exec.Target {
	t := exec.Target{
		Task: task.ID, Service: task.ServiceID, ServiceName: task.ServiceID, Node: task.NodeID,
	}
	if task.Status.ContainerStatus != nil {
		t.Container = task.Status.ContainerStatus.ContainerID
	}
	if svc, ok := h.cache.GetService(task.ServiceID); ok {
		t.ServiceName = svc.Spec.Name
		t.Stack = svc.Spec.Labels["com.docker.stack.namespace"]
	}

	return t
}

// execResource is what an exec grant must cover: the resource a write to the
// target's task is checked against. TaskRecord reads only the two IDs.
func (h *Handlers) execResource(t exec.Target) string {
	return cluster.TaskRecord(h.cache, swarm.Task{ID: t.Task, ServiceID: t.Service}).WriteResource()
}

// execDecision runs every check a new session must pass, in the order their
// refusals should be reported. Allow on /tasks/{id}/exec is its answer.
func (h *Handlers) execDecision(r *http.Request, task swarm.Task) (exec.Transport, *execRefusal) {
	if level := h.levelFor(r); level < cluster.OpOpenExec.Tier() {
		return nil, &execRefusal{"OPS001", levelRefusal(cluster.OpOpenExec.Tier(), level)}
	}

	id := auth.IdentityFromContext(r.Context())
	if !h.acl.Can(id, acl.PermissionExec, h.execResource(h.execTarget(task))) {
		return nil, &execRefusal{"ACL003", "no exec grant covers this task's service"}
	}

	node, found := h.cache.GetNode(task.NodeID)
	if reason := cluster.ExecEligibility(
		task,
		node,
		found,
		h.exec.Self,
	); reason != cluster.ExecEligible {
		return nil, &execRefusal{"EXE001", string(reason)}
	}

	transport, ok := exec.Select(h.exec.Transports, task.NodeID)
	if !ok {
		return nil, &execRefusal{"EXE002", "no shell transport reaches node " + task.NodeID}
	}

	return transport, nil
}

// mayOpenExec reports whether POST /tasks/{id}/exec would get past its checks.
func (h *Handlers) mayOpenExec(r *http.Request) bool {
	task, ok := h.cache.GetTask(r.PathValue("id"))
	if !ok {
		return false
	}
	_, refusal := h.execDecision(r, task)

	return refusal == nil
}

// mayEndExec reports whether DELETE /exec/{sid} would find the session.
func (h *Handlers) mayEndExec(r *http.Request) bool {
	s, ok := h.exec.Manager.Get(r.PathValue("sid"))

	return ok && h.canSeeSession(r, s)
}

// ExecAuthorizer re-checks a live session's grant after a policy reload.
func (h *Handlers) ExecAuthorizer() func(*auth.Identity, exec.Target) bool {
	return func(id *auth.Identity, target exec.Target) bool {
		return h.acl.Can(id, acl.PermissionExec, h.execResource(target))
	}
}

// canSeeSession is the visibility rule for listing, reading and ending.
func (h *Handlers) canSeeSession(r *http.Request, s exec.Session) bool {
	id := auth.IdentityFromContext(r.Context())

	return s.Owner.Is(id) || h.acl.Can(id, acl.PermissionExec, h.execResource(s.Target))
}

func sessionPath(sid string) string { return "/exec/" + sid }

func taskReadResource(t swarm.Task) string { return "task:" + t.ID }

func (h *Handlers) HandleTaskExec(w http.ResponseWriter, r *http.Request) {
	task, ok := lookupACL(h, w, r, "task", r.PathValue("id"), h.cache.GetTask, taskReadResource)
	if !ok {
		return
	}

	methods := "GET, HEAD"
	if _, refusal := h.execDecision(r, task); refusal == nil {
		methods += ", POST"
	}
	w.Header().Set("Allow", methods)
	varyByIdentity(w)

	var sessions []exec.Session
	for _, s := range h.exec.Manager.List() {
		if s.Target.Task == task.ID && h.canSeeSession(r, s) {
			sessions = append(sessions, s)
		}
	}
	h.writeSessionCollection(w, r, sessions)
}

func (h *Handlers) HandleListExecSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "GET, HEAD")
	varyByIdentity(w)

	var sessions []exec.Session
	for _, s := range h.exec.Manager.List() {
		if h.canSeeSession(r, s) {
			sessions = append(sessions, s)
		}
	}
	h.writeSessionCollection(w, r, sessions)
}

func (h *Handlers) writeSessionCollection(
	w http.ResponseWriter,
	r *http.Request,
	sessions []exec.Session,
) {
	p, err := parsePagination(r)
	if err != nil {
		problem.Write(w, r, "API016", err.Error())
		return
	}

	total := len(sessions)
	page := sessions[min(p.Offset, total):min(p.Offset+p.Limit, total)]
	items := wrapItems(r.Context(), page, "ExecSession", func(s exec.Session) string {
		return sessionPath(s.ID)
	})
	writeCollectionResponse(
		w, r, NewCollectionResponse(r.Context(), items, total, p.Limit, p.Offset), p, "",
	)
}

func (h *Handlers) HandleCreateExecSession(w http.ResponseWriter, r *http.Request) {
	task, ok := lookupACL(h, w, r, "task", r.PathValue("id"), h.cache.GetTask, taskReadResource)
	if !ok {
		return
	}

	id := auth.IdentityFromContext(r.Context())
	target := h.execTarget(task)
	transport, refusal := h.execDecision(r, task)
	if refusal != nil {
		h.exec.Manager.Denied(id, clientIP(r), target, refusal.code)
		problem.Write(w, r, refusal.code, refusal.detail)
		return
	}

	session, err := h.exec.Manager.Create(id, clientIP(r), target, transport.Name())
	if errors.Is(err, exec.ErrCapacity) || errors.Is(err, exec.ErrIdentityCapacity) {
		h.exec.Manager.Denied(id, clientIP(r), target, "EXE003")
		w.Header().Set("Retry-After", sse.RetryAfter())
		problem.Write(w, r, "EXE003", err.Error())
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "exec session not created", "error", err)
		problem.WriteStatus(w, r, http.StatusInternalServerError, "exec session not created")
		return
	}

	w.Header().Set("Location", absPath(r.Context(), sessionPath(session.ID)))
	if preferMinimal(r) {
		writePreferCreated(w)
		return
	}

	// Not the cached writer: a precondition must never turn a create into a 304.
	writeJSONStatus(w, http.StatusCreated,
		NewDetailResponse(r.Context(), sessionPath(session.ID), "ExecSession", session))
}

// sessionFor resolves {sid} to a session the caller may see, or writes the 404:
// an invisible session is not confirmed to exist.
func (h *Handlers) sessionFor(w http.ResponseWriter, r *http.Request) (exec.Session, bool) {
	s, ok := h.exec.Manager.Get(r.PathValue("sid"))
	if !ok || !h.canSeeSession(r, s) {
		problem.Write(w, r, "EXE006", "exec session not found")
		return exec.Session{}, false
	}

	return s, true
}

func (h *Handlers) HandleGetExecSession(w http.ResponseWriter, r *http.Request) {
	s, ok := h.sessionFor(w, r)
	if !ok {
		return
	}

	methods := "GET, HEAD"
	if h.levelFor(r) >= cluster.OpEndExec.Tier() {
		methods += ", DELETE"
	}
	w.Header().Set("Allow", methods)
	varyByIdentity(w)

	writeCachedJSON(w, r, NewDetailResponse(r.Context(), sessionPath(s.ID), "ExecSession", s))
}

func (h *Handlers) HandleDeleteExecSession(w http.ResponseWriter, r *http.Request) {
	s, ok := h.sessionFor(w, r)
	if !ok {
		return
	}

	h.exec.Manager.End(s.ID, "terminated_by", auth.IdentityFromContext(r.Context()))
	w.WriteHeader(http.StatusNoContent)
}

// execSessionEntry is where the terminal WebSocket upgrade branches off the
// session's own representation.
func (h *Handlers) execSessionEntry(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && isWebSocketUpgrade(r) {
			h.attachExecSession(w, r)
			return
		}
		next(w, r)
	}
}
