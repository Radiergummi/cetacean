package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/docker/docker/api/types/swarm"
)

func (h *Handlers) HandlePatchSwarmOrchestration(w http.ResponseWriter, r *http.Request) {
	if !requireMergePatch(w, r) {
		return
	}

	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	current, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		writeErrorCode(w, r, "SWM002", "failed to inspect swarm")
		return
	}

	patch, ok := decodeJSON[swarm.OrchestrationConfig](w, r)
	if !ok {
		return
	}

	spec := current.Spec
	if patch.TaskHistoryRetentionLimit != nil {
		spec.Orchestration.TaskHistoryRetentionLimit = patch.TaskHistoryRetentionLimit
	}

	slog.Info("updating swarm orchestration config")

	if err := h.systemClient.UpdateSwarm(
		ctx,
		spec,
		current.Version,
		swarm.UpdateFlags{},
	); err != nil {
		writeErrorCode(w, r, "SWM003", "failed to update swarm: "+err.Error())
		return
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		r.URL.Path,
		"SwarmOrchestration",
		map[string]any{"orchestration": spec.Orchestration},
	))
}

func (h *Handlers) HandlePatchSwarmRaft(w http.ResponseWriter, r *http.Request) {
	if !requireMergePatch(w, r) {
		return
	}

	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	current, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		writeErrorCode(w, r, "SWM002", "failed to inspect swarm")
		return
	}

	patch, ok := decodeJSON[swarm.RaftConfig](w, r)
	if !ok {
		return
	}

	spec := current.Spec
	if patch.SnapshotInterval != 0 {
		spec.Raft.SnapshotInterval = patch.SnapshotInterval
	}
	if patch.KeepOldSnapshots != nil {
		spec.Raft.KeepOldSnapshots = patch.KeepOldSnapshots
	}
	if patch.LogEntriesForSlowFollowers != 0 {
		spec.Raft.LogEntriesForSlowFollowers = patch.LogEntriesForSlowFollowers
	}

	slog.Info("updating swarm raft config")

	if err := h.systemClient.UpdateSwarm(
		ctx,
		spec,
		current.Version,
		swarm.UpdateFlags{},
	); err != nil {
		writeErrorCode(w, r, "SWM003", "failed to update swarm: "+err.Error())
		return
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		r.URL.Path,
		"SwarmRaft",
		map[string]any{"raft": spec.Raft},
	))
}

func (h *Handlers) HandlePatchSwarmDispatcher(w http.ResponseWriter, r *http.Request) {
	if !requireMergePatch(w, r) {
		return
	}

	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	current, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		writeErrorCode(w, r, "SWM002", "failed to inspect swarm")
		return
	}

	patch, ok := decodeJSON[swarm.DispatcherConfig](w, r)
	if !ok {
		return
	}

	spec := current.Spec
	if patch.HeartbeatPeriod != 0 {
		spec.Dispatcher.HeartbeatPeriod = patch.HeartbeatPeriod
	}

	slog.Info("updating swarm dispatcher config")

	if err := h.systemClient.UpdateSwarm(
		ctx,
		spec,
		current.Version,
		swarm.UpdateFlags{},
	); err != nil {
		writeErrorCode(w, r, "SWM003", "failed to update swarm: "+err.Error())
		return
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		r.URL.Path,
		"SwarmDispatcher",
		map[string]any{"dispatcher": spec.Dispatcher},
	))
}

func (h *Handlers) HandlePatchSwarmCAConfig(w http.ResponseWriter, r *http.Request) {
	if !requireMergePatch(w, r) {
		return
	}

	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	current, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		writeErrorCode(w, r, "SWM002", "failed to inspect swarm")
		return
	}

	patch, ok := decodeJSON[swarm.CAConfig](w, r)
	if !ok {
		return
	}

	spec := current.Spec
	if patch.NodeCertExpiry != 0 {
		spec.CAConfig.NodeCertExpiry = patch.NodeCertExpiry
	}

	slog.Info("updating swarm CA config")

	if err := h.systemClient.UpdateSwarm(
		ctx,
		spec,
		current.Version,
		swarm.UpdateFlags{},
	); err != nil {
		writeErrorCode(w, r, "SWM003", "failed to update swarm: "+err.Error())
		return
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		r.URL.Path,
		"SwarmCAConfig",
		map[string]any{"caConfig": spec.CAConfig},
	))
}

func (h *Handlers) HandlePatchSwarmEncryption(w http.ResponseWriter, r *http.Request) {
	if !requireMergePatch(w, r) {
		return
	}

	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	current, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		writeErrorCode(w, r, "SWM002", "failed to inspect swarm")
		return
	}

	patch, ok := decodeJSON[struct {
		AutoLockManagers *bool `json:"AutoLockManagers"`
	}](w, r)
	if !ok {
		return
	}

	spec := current.Spec
	if patch.AutoLockManagers != nil {
		spec.EncryptionConfig.AutoLockManagers = *patch.AutoLockManagers
	}

	slog.Info("updating swarm encryption config")

	if err := h.systemClient.UpdateSwarm(
		ctx,
		spec,
		current.Version,
		swarm.UpdateFlags{},
	); err != nil {
		writeErrorCode(w, r, "SWM003", "failed to update swarm: "+err.Error())
		return
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		r.URL.Path,
		"SwarmEncryption",
		map[string]any{"encryption": spec.EncryptionConfig},
	))
}

func (h *Handlers) HandlePostRotateToken(w http.ResponseWriter, r *http.Request) {
	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	req, ok := decodeJSON[struct {
		Target string `json:"target"`
	}](w, r)
	if !ok {
		return
	}

	var flags swarm.UpdateFlags
	switch req.Target {
	case "worker":
		flags.RotateWorkerToken = true
	case "manager":
		flags.RotateManagerToken = true
	default:
		writeErrorCode(w, r, "SWM011", "target must be \"worker\" or \"manager\"")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	current, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		writeErrorCode(w, r, "SWM002", "failed to inspect swarm")
		return
	}

	slog.Info("rotating swarm join token", "target", req.Target)

	if err := h.systemClient.UpdateSwarm(ctx, current.Spec, current.Version, flags); err != nil {
		writeErrorCode(w, r, "SWM006", "failed to rotate token: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandlePostRotateUnlockKey(w http.ResponseWriter, r *http.Request) {
	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	current, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		writeErrorCode(w, r, "SWM002", "failed to inspect swarm")
		return
	}

	slog.Info("rotating swarm unlock key")

	flags := swarm.UpdateFlags{RotateManagerUnlockKey: true}
	if err := h.systemClient.UpdateSwarm(ctx, current.Spec, current.Version, flags); err != nil {
		writeErrorCode(
			w,
			r,
			"SWM007",
			"failed to rotate unlock key: "+err.Error(),
		)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandlePostForceRotateCA(w http.ResponseWriter, r *http.Request) {
	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	current, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		writeErrorCode(w, r, "SWM002", "failed to inspect swarm")
		return
	}

	spec := current.Spec
	spec.CAConfig.ForceRotate++

	slog.Info("forcing CA certificate rotation", "forceRotate", spec.CAConfig.ForceRotate)

	if err := h.systemClient.UpdateSwarm(
		ctx,
		spec,
		current.Version,
		swarm.UpdateFlags{},
	); err != nil {
		writeErrorCode(
			w,
			r,
			"SWM003",
			"failed to force CA rotation: "+err.Error(),
		)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleGetUnlockKey(w http.ResponseWriter, r *http.Request) {
	// ACL write check is handled by swarmACL middleware in the router.
	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	key, err := h.systemClient.GetUnlockKey(ctx)
	if err != nil {
		writeErrorCode(w, r, "SWM009", "failed to get unlock key: "+err.Error())
		return
	}

	writeCachedJSON(
		w,
		r,
		NewDetailResponse(
			r.Context(),
			"/swarm/unlock-key",
			"SwarmUnlockKey",
			map[string]any{"unlockKey": key},
		),
	)
}

func (h *Handlers) HandlePostUnlockSwarm(w http.ResponseWriter, r *http.Request) {
	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	body, ok := decodeJSON[struct {
		UnlockKey string `json:"unlockKey"`
	}](w, r)
	if !ok {
		return
	}

	if body.UnlockKey == "" {
		writeErrorCode(w, r, "SWM010", "unlockKey is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	slog.Info("unlocking swarm")

	if err := h.systemClient.UnlockSwarm(ctx, body.UnlockKey); err != nil {
		writeErrorCode(w, r, "SWM008", "failed to unlock swarm: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
