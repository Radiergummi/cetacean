package api

import (
	"fmt"
	"log/slog"
	"net/http"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/auth"
)

type updateAvailabilityRequest struct {
	Availability string `json:"availability"`
}

func (h *Handlers) HandleUpdateNodeAvailability(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit

	var req updateAvailabilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}

	var availability swarm.NodeAvailability
	switch req.Availability {
	case "active":
		availability = swarm.NodeAvailabilityActive
	case "drain":
		availability = swarm.NodeAvailabilityDrain
	case "pause":
		availability = swarm.NodeAvailabilityPause
	default:
		writeErrorCode(
			w,
			r,
			"NOD004",
			"availability must be one of: active, drain, pause",
		)
		return
	}

	_, ok := h.cache.GetNode(id)
	if !ok {
		writeErrorCode(w, r, "NOD003", fmt.Sprintf("node %q not found", id))
		return
	}

	slog.Info("updating node availability", "node", id, "availability", req.Availability)

	writeNodeMutation(w, r, id, func() (swarm.Node, error) {
		return h.nodeWriter.UpdateNodeAvailability(r.Context(), id, availability)
	})
}

type updateRoleRequest struct {
	Role string `json:"role"`
}

func (h *Handlers) HandleUpdateNodeRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req updateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}

	var role swarm.NodeRole
	switch req.Role {
	case "worker":
		role = swarm.NodeRoleWorker
	case "manager":
		role = swarm.NodeRoleManager
	default:
		writeErrorCode(w, r, "NOD005", "role must be one of: worker, manager")
		return
	}

	_, ok := h.cache.GetNode(id)
	if !ok {
		writeErrorCode(w, r, "NOD003", fmt.Sprintf("node %q not found", id))
		return
	}

	slog.Info("updating node role", "node", id, "role", req.Role)

	writeNodeMutation(w, r, id, func() (swarm.Node, error) {
		return h.nodeWriter.UpdateNodeRole(r.Context(), id, role)
	})
}

func (h *Handlers) HandleRemoveNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetNode(id)
	if !ok {
		writeErrorCode(w, r, "NOD003", fmt.Sprintf("node %q not found", id))
		return
	}

	force := r.URL.Query().Get("force") == "true"

	slog.Info("removing node", "node", id, "force", force)

	err := h.nodeWriter.RemoveNode(r.Context(), id, force)
	if err != nil {
		if !force && (cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err)) {
			writeErrorCode(w, r, "NOD001", err.Error())
			return
		}
		writeDockerError(w, r, err, "node", id)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleGetNodeRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	node, ok := h.cache.GetNode(id)
	if !ok {
		writeErrorCode(w, r, "NOD003", fmt.Sprintf("node %q not found", id))
		return
	}
	if !h.acl.Can(auth.IdentityFromContext(r.Context()), "read", nodeResource(node)) {
		writeErrorCode(w, r, "ACL001", "access denied")
		return
	}

	managerCount := 0
	for _, n := range h.cache.ListNodes() {
		if n.Spec.Role == swarm.NodeRoleManager {
			managerCount++
		}
	}

	writeCachedJSON(w, r, NodeRoleResponse{
		Role:         string(node.Spec.Role),
		IsLeader:     node.ManagerStatus != nil && node.ManagerStatus.Leader,
		ManagerCount: managerCount,
	})
}

func (h *Handlers) HandleGetNodeLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, ok := h.cache.GetNode(id)
	if !ok {
		writeErrorCode(w, r, "NOD003", fmt.Sprintf("node %q not found", id))
		return
	}
	if !h.acl.Can(auth.IdentityFromContext(r.Context()), "read", nodeResource(node)) {
		writeErrorCode(w, r, "ACL001", "access denied")
		return
	}
	labels := node.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeCachedJSON(
		w,
		r,
		NewDetailResponse(r.Context(), "/nodes/"+id+"/labels", "NodeLabels", LabelsResponse{
			Labels: labels,
		}),
	)
}

func (h *Handlers) HandlePatchNodeLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit

	node, ok := h.cache.GetNode(id)
	if !ok {
		writeErrorCode(w, r, "NOD003", fmt.Sprintf("node %q not found", id))
		return
	}

	updated, ok := patchStringMap(w, r, node.Spec.Labels)
	if !ok {
		return
	}

	slog.Info("patching node labels", "node", id)

	result, err := h.nodeWriter.UpdateNodeLabels(r.Context(), id, updated)
	if err != nil {
		writeNodeError(w, r, err, id)
		return
	}

	labels := result.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeMutationResponse(w, r, labels)
}
