package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/docker"
)

// swarmFailure is the error code and message a failed swarm update answers with.
type swarmFailure struct {
	code, message string
}

var swarmUpdateFailed = swarmFailure{"SWM003", "failed to update swarm"}

// updateSwarm applies apply to the current swarm spec and writes it back at the
// version it was read at, answering any failure itself. Callers decode their
// input first, so a bad request never costs an engine call.
func (h *Handlers) updateSwarm(
	w http.ResponseWriter,
	r *http.Request,
	apply func(spec *swarm.Spec) swarm.UpdateFlags,
	failed swarmFailure,
) (swarm.Spec, bool) {
	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return swarm.Spec{}, false
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	current, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		writeErrorCode(w, r, "SWM002", "failed to inspect swarm")
		return swarm.Spec{}, false
	}

	spec := current.Spec
	flags := apply(&spec)

	if err := h.systemClient.UpdateSwarm(ctx, spec, current.Version, flags); err != nil {
		writeSwarmErrorCode(w, r, err, failed.code, failed.message)
		return swarm.Spec{}, false
	}

	return spec, true
}

func (h *Handlers) HandlePatchSwarmOrchestration(w http.ResponseWriter, r *http.Request) {
	if !requireMergePatch(w, r) {
		return
	}

	patch, ok := decodeJSON[swarm.OrchestrationConfig](w, r)
	if !ok {
		return
	}

	spec, ok := h.updateSwarm(w, r, func(spec *swarm.Spec) swarm.UpdateFlags {
		if patch.TaskHistoryRetentionLimit != nil {
			spec.Orchestration.TaskHistoryRetentionLimit = patch.TaskHistoryRetentionLimit
		}

		slog.Info("updating swarm orchestration config")

		return swarm.UpdateFlags{}
	}, swarmUpdateFailed)
	if !ok {
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

	patch, ok := decodeJSON[swarm.RaftConfig](w, r)
	if !ok {
		return
	}

	spec, ok := h.updateSwarm(w, r, func(spec *swarm.Spec) swarm.UpdateFlags {
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

		return swarm.UpdateFlags{}
	}, swarmUpdateFailed)
	if !ok {
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

	patch, ok := decodeJSON[swarm.DispatcherConfig](w, r)
	if !ok {
		return
	}

	spec, ok := h.updateSwarm(w, r, func(spec *swarm.Spec) swarm.UpdateFlags {
		if patch.HeartbeatPeriod != 0 {
			spec.Dispatcher.HeartbeatPeriod = patch.HeartbeatPeriod
		}

		slog.Info("updating swarm dispatcher config")

		return swarm.UpdateFlags{}
	}, swarmUpdateFailed)
	if !ok {
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

	patch, ok := decodeJSON[swarm.CAConfig](w, r)
	if !ok {
		return
	}

	spec, ok := h.updateSwarm(w, r, func(spec *swarm.Spec) swarm.UpdateFlags {
		if patch.NodeCertExpiry != 0 {
			spec.CAConfig.NodeCertExpiry = patch.NodeCertExpiry
		}

		slog.Info("updating swarm CA config")

		return swarm.UpdateFlags{}
	}, swarmUpdateFailed)
	if !ok {
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

	patch, ok := decodeJSON[struct {
		AutoLockManagers *bool `json:"AutoLockManagers"`
	}](w, r)
	if !ok {
		return
	}

	spec, ok := h.updateSwarm(w, r, func(spec *swarm.Spec) swarm.UpdateFlags {
		if patch.AutoLockManagers != nil {
			spec.EncryptionConfig.AutoLockManagers = *patch.AutoLockManagers
		}

		slog.Info("updating swarm encryption config")

		return swarm.UpdateFlags{}
	}, swarmUpdateFailed)
	if !ok {
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

	if _, ok := h.updateSwarm(w, r, func(*swarm.Spec) swarm.UpdateFlags {
		slog.Info("rotating swarm join token", "target", req.Target)

		return flags
	}, swarmFailure{"SWM006", "failed to rotate token"}); !ok {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandlePostRotateUnlockKey(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.updateSwarm(w, r, func(*swarm.Spec) swarm.UpdateFlags {
		slog.Info("rotating swarm unlock key")

		return swarm.UpdateFlags{RotateManagerUnlockKey: true}
	}, swarmFailure{"SWM007", "failed to rotate unlock key"}); !ok {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandlePostForceRotateCA(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.updateSwarm(w, r, func(spec *swarm.Spec) swarm.UpdateFlags {
		spec.CAConfig.ForceRotate++

		slog.Info("forcing CA certificate rotation", "force_rotate", spec.CAConfig.ForceRotate)

		return swarm.UpdateFlags{}
	}, swarmFailure{"SWM003", "failed to force CA rotation"}); !ok {
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
		writeSwarmErrorCode(w, r, err, "SWM009", "failed to get unlock key")
		return
	}

	// The key decrypts the managers' Raft logs: no cache may keep it.
	w.Header().Set("Cache-Control", "no-store")
	varyByIdentity(w)

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
		writeSwarmErrorCode(w, r, err, "SWM008", "failed to unlock swarm")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// writeSwarmErrorCode answers a failed swarm update. Only a refusal the caller
// can act on keeps the daemon's text; anything else may name hosts and
// internals, so it is logged and the response says only what failed.
func writeSwarmErrorCode(
	w http.ResponseWriter,
	r *http.Request,
	err error,
	code, failed string,
) {
	if writeSwarmConflict(w, r, err) {
		return
	}

	if cerrdefs.IsInvalidArgument(err) || cerrdefs.IsUnavailable(err) {
		writeErrorCode(w, r, code, failed+": "+err.Error())
		return
	}

	slog.Error(failed, "error", err)
	writeErrorCode(w, r, code, failed)
}

// writeSwarmConflict answers a write that lost the race against another to the
// swarm's version, and reports whether it did.
func writeSwarmConflict(w http.ResponseWriter, r *http.Request, err error) bool {
	if !docker.IsOutOfSequence(err) {
		return false
	}

	writeErrorCode(w, r, "SWM012", "the swarm changed while it was being updated")

	return true
}
