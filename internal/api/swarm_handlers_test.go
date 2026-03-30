package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"
)

type mockSystemClient struct {
	swarmInspectFn func(ctx context.Context) (swarm.Swarm, error)
	diskUsageFn    func(ctx context.Context) (types.DiskUsage, error)
	localNodeIDFn  func(ctx context.Context) (string, error)
	updateSwarmFn  func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error
	getUnlockKeyFn func(ctx context.Context) (string, error)
	unlockSwarmFn  func(ctx context.Context, key string) error
}

func (m *mockSystemClient) SwarmInspect(ctx context.Context) (swarm.Swarm, error) {
	if m.swarmInspectFn != nil {
		return m.swarmInspectFn(ctx)
	}
	return swarm.Swarm{}, fmt.Errorf("not implemented")
}

func (m *mockSystemClient) DiskUsage(ctx context.Context) (types.DiskUsage, error) {
	if m.diskUsageFn != nil {
		return m.diskUsageFn(ctx)
	}
	return types.DiskUsage{}, fmt.Errorf("not implemented")
}

func (m *mockSystemClient) LocalNodeID(ctx context.Context) (string, error) {
	if m.localNodeIDFn != nil {
		return m.localNodeIDFn(ctx)
	}
	return "", fmt.Errorf("not implemented")
}

func (m *mockSystemClient) UpdateSwarm(
	ctx context.Context,
	spec swarm.Spec,
	version swarm.Version,
	flags swarm.UpdateFlags,
) error {
	if m.updateSwarmFn != nil {
		return m.updateSwarmFn(ctx, spec, version, flags)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockSystemClient) GetUnlockKey(ctx context.Context) (string, error) {
	if m.getUnlockKeyFn != nil {
		return m.getUnlockKeyFn(ctx)
	}
	return "", fmt.Errorf("not implemented")
}

func (m *mockSystemClient) UnlockSwarm(ctx context.Context, key string) error {
	if m.unlockSwarmFn != nil {
		return m.unlockSwarmFn(ctx, key)
	}
	return fmt.Errorf("not implemented")
}

// Compile-time check: mockSystemClient must satisfy DockerSystemClient.
var _ DockerSystemClient = (*mockSystemClient)(nil)

func newSwarmHandlers(t testing.TB, sc DockerSystemClient) *Handlers {
	t.Helper()
	return newTestHandlers(t, withSystemClient(sc))
}

func validSwarm() swarm.Swarm {
	retention := int64(5)
	keepOld := uint64(1)
	return swarm.Swarm{
		ClusterInfo: swarm.ClusterInfo{
			Meta: swarm.Meta{
				Version: swarm.Version{Index: 42},
			},
			Spec: swarm.Spec{
				Orchestration: swarm.OrchestrationConfig{
					TaskHistoryRetentionLimit: &retention,
				},
				Raft: swarm.RaftConfig{
					SnapshotInterval:           10000,
					KeepOldSnapshots:           &keepOld,
					LogEntriesForSlowFollowers: 500,
					ElectionTick:               10,
					HeartbeatTick:              1,
				},
				Dispatcher: swarm.DispatcherConfig{
					HeartbeatPeriod: 5 * time.Second,
				},
				CAConfig: swarm.CAConfig{
					NodeCertExpiry: 90 * 24 * time.Hour,
				},
				EncryptionConfig: swarm.EncryptionConfig{
					AutoLockManagers: false,
				},
			},
		},
	}
}

func noopUpdate() func(context.Context, swarm.Spec, swarm.Version, swarm.UpdateFlags) error {
	return func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error {
		return nil
	}
}

// --- Shared error helpers ---

func inspectError() func(context.Context) (swarm.Swarm, error) {
	return func(ctx context.Context) (swarm.Swarm, error) {
		return swarm.Swarm{}, fmt.Errorf("connection refused")
	}
}

func updateError() func(context.Context, swarm.Spec, swarm.Version, swarm.UpdateFlags) error {
	return func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error {
		return fmt.Errorf("raft: leadership lost")
	}
}

func mergePatchRequest(path, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	return req
}

// --- HandlePatchSwarmOrchestration ---

func TestHandleSwarmOrchestration_Success(t *testing.T) {
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: noopUpdate(),
	}
	h := newSwarmHandlers(t, sc)

	body := `{"TaskHistoryRetentionLimit": 10}`
	req := httptest.NewRequest(http.MethodPatch, "/swarm/orchestration", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	rec := httptest.NewRecorder()

	h.HandlePatchSwarmOrchestration(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	var orch swarm.OrchestrationConfig
	if err := json.Unmarshal(resp["orchestration"], &orch); err != nil {
		t.Fatal(err)
	}
	if orch.TaskHistoryRetentionLimit == nil || *orch.TaskHistoryRetentionLimit != 10 {
		t.Fatalf("TaskHistoryRetentionLimit=%v, want 10", orch.TaskHistoryRetentionLimit)
	}
}

func TestHandleSwarmOrchestration_NilClient(t *testing.T) {
	h := newSwarmHandlers(t, nil)
	req := httptest.NewRequest(http.MethodPatch, "/swarm/orchestration", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	rec := httptest.NewRecorder()

	h.HandlePatchSwarmOrchestration(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestHandleSwarmOrchestration_WrongContentType(t *testing.T) {
	h := newSwarmHandlers(t, &mockSystemClient{})
	req := httptest.NewRequest(http.MethodPatch, "/swarm/orchestration", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandlePatchSwarmOrchestration(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusUnsupportedMediaType)
	}
}

func TestHandleSwarmOrchestration_InspectError(t *testing.T) {
	sc := &mockSystemClient{swarmInspectFn: inspectError()}
	h := newSwarmHandlers(t, sc)

	rec := httptest.NewRecorder()
	h.HandlePatchSwarmOrchestration(rec, mergePatchRequest("/swarm/orchestration", `{}`))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleSwarmOrchestration_UpdateError(t *testing.T) {
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: updateError(),
	}
	h := newSwarmHandlers(t, sc)

	rec := httptest.NewRecorder()
	h.HandlePatchSwarmOrchestration(
		rec,
		mergePatchRequest("/swarm/orchestration", `{"TaskHistoryRetentionLimit": 10}`),
	)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf(
			"status=%d, want %d; body: %s",
			rec.Code,
			http.StatusInternalServerError,
			rec.Body.String(),
		)
	}
}

func TestHandleSwarmOrchestration_InvalidBody(t *testing.T) {
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
	}
	h := newSwarmHandlers(t, sc)

	rec := httptest.NewRecorder()
	h.HandlePatchSwarmOrchestration(rec, mergePatchRequest("/swarm/orchestration", "not json"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleSwarmOrchestration_NilRetention_Preserved(t *testing.T) {
	var capturedSpec swarm.Spec
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error {
			capturedSpec = spec
			return nil
		},
	}
	h := newSwarmHandlers(t, sc)

	// Empty patch should preserve existing retention limit
	rec := httptest.NewRecorder()
	h.HandlePatchSwarmOrchestration(rec, mergePatchRequest("/swarm/orchestration", `{}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if capturedSpec.Orchestration.TaskHistoryRetentionLimit == nil ||
		*capturedSpec.Orchestration.TaskHistoryRetentionLimit != 5 {
		t.Fatalf(
			"TaskHistoryRetentionLimit=%v, want 5 (preserved)",
			capturedSpec.Orchestration.TaskHistoryRetentionLimit,
		)
	}
}

func TestHandleSwarmOrchestration_VersionPassedThrough(t *testing.T) {
	var capturedVersion swarm.Version
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error {
			capturedVersion = version
			return nil
		},
	}
	h := newSwarmHandlers(t, sc)

	rec := httptest.NewRecorder()
	h.HandlePatchSwarmOrchestration(
		rec,
		mergePatchRequest("/swarm/orchestration", `{"TaskHistoryRetentionLimit": 10}`),
	)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusOK)
	}
	if capturedVersion.Index != 42 {
		t.Fatalf("version.Index=%d, want 42", capturedVersion.Index)
	}
}

// --- HandlePatchSwarmRaft ---

func TestHandleSwarmRaft_Success(t *testing.T) {
	var capturedSpec swarm.Spec
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error {
			capturedSpec = spec
			return nil
		},
	}
	h := newSwarmHandlers(t, sc)

	body := `{"SnapshotInterval": 20000}`
	req := httptest.NewRequest(http.MethodPatch, "/swarm/raft", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	rec := httptest.NewRecorder()

	h.HandlePatchSwarmRaft(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusOK)
	}

	// Verify snapshot interval was updated
	if capturedSpec.Raft.SnapshotInterval != 20000 {
		t.Fatalf("SnapshotInterval=%d, want 20000", capturedSpec.Raft.SnapshotInterval)
	}

	// Verify ElectionTick and HeartbeatTick were preserved (not zeroed)
	if capturedSpec.Raft.ElectionTick != 10 {
		t.Fatalf("ElectionTick=%d, want 10 (preserved)", capturedSpec.Raft.ElectionTick)
	}
	if capturedSpec.Raft.HeartbeatTick != 1 {
		t.Fatalf("HeartbeatTick=%d, want 1 (preserved)", capturedSpec.Raft.HeartbeatTick)
	}
}

func TestHandleSwarmRaft_NilClient(t *testing.T) {
	h := newSwarmHandlers(t, nil)
	req := httptest.NewRequest(http.MethodPatch, "/swarm/raft", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	rec := httptest.NewRecorder()

	h.HandlePatchSwarmRaft(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestHandleSwarmRaft_WrongContentType(t *testing.T) {
	h := newSwarmHandlers(t, &mockSystemClient{})
	req := httptest.NewRequest(http.MethodPatch, "/swarm/raft", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandlePatchSwarmRaft(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusUnsupportedMediaType)
	}
}

func TestHandleSwarmRaft_InspectError(t *testing.T) {
	sc := &mockSystemClient{swarmInspectFn: inspectError()}
	h := newSwarmHandlers(t, sc)

	rec := httptest.NewRecorder()
	h.HandlePatchSwarmRaft(rec, mergePatchRequest("/swarm/raft", `{}`))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleSwarmRaft_UpdateError(t *testing.T) {
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: updateError(),
	}
	h := newSwarmHandlers(t, sc)

	rec := httptest.NewRecorder()
	h.HandlePatchSwarmRaft(rec, mergePatchRequest("/swarm/raft", `{"SnapshotInterval": 20000}`))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestHandleSwarmRaft_InvalidBody(t *testing.T) {
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
	}
	h := newSwarmHandlers(t, sc)

	rec := httptest.NewRecorder()
	h.HandlePatchSwarmRaft(rec, mergePatchRequest("/swarm/raft", "not json"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleSwarmRaft_MultiField(t *testing.T) {
	var capturedSpec swarm.Spec
	keepOld := uint64(3)
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error {
			capturedSpec = spec
			return nil
		},
	}
	h := newSwarmHandlers(t, sc)

	body := fmt.Sprintf(
		`{"SnapshotInterval": 20000, "KeepOldSnapshots": %d, "LogEntriesForSlowFollowers": 1000}`,
		keepOld,
	)
	rec := httptest.NewRecorder()
	h.HandlePatchSwarmRaft(rec, mergePatchRequest("/swarm/raft", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if capturedSpec.Raft.SnapshotInterval != 20000 {
		t.Fatalf("SnapshotInterval=%d, want 20000", capturedSpec.Raft.SnapshotInterval)
	}
	if capturedSpec.Raft.KeepOldSnapshots == nil || *capturedSpec.Raft.KeepOldSnapshots != 3 {
		t.Fatalf("KeepOldSnapshots=%v, want 3", capturedSpec.Raft.KeepOldSnapshots)
	}
	if capturedSpec.Raft.LogEntriesForSlowFollowers != 1000 {
		t.Fatalf(
			"LogEntriesForSlowFollowers=%d, want 1000",
			capturedSpec.Raft.LogEntriesForSlowFollowers,
		)
	}
	// Preserved fields
	if capturedSpec.Raft.ElectionTick != 10 {
		t.Fatalf("ElectionTick=%d, want 10 (preserved)", capturedSpec.Raft.ElectionTick)
	}
	if capturedSpec.Raft.HeartbeatTick != 1 {
		t.Fatalf("HeartbeatTick=%d, want 1 (preserved)", capturedSpec.Raft.HeartbeatTick)
	}
}

func TestHandleSwarmRaft_EmptyPatch_PreservesAll(t *testing.T) {
	var capturedSpec swarm.Spec
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error {
			capturedSpec = spec
			return nil
		},
	}
	h := newSwarmHandlers(t, sc)

	rec := httptest.NewRecorder()
	h.HandlePatchSwarmRaft(rec, mergePatchRequest("/swarm/raft", `{}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusOK)
	}
	// All original values should be preserved
	if capturedSpec.Raft.SnapshotInterval != 10000 {
		t.Fatalf("SnapshotInterval=%d, want 10000 (preserved)", capturedSpec.Raft.SnapshotInterval)
	}
	if capturedSpec.Raft.LogEntriesForSlowFollowers != 500 {
		t.Fatalf(
			"LogEntriesForSlowFollowers=%d, want 500 (preserved)",
			capturedSpec.Raft.LogEntriesForSlowFollowers,
		)
	}
}

// --- HandlePatchSwarmDispatcher ---

func TestHandleSwarmDispatcher_Success(t *testing.T) {
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: noopUpdate(),
	}
	h := newSwarmHandlers(t, sc)

	body := fmt.Sprintf(`{"HeartbeatPeriod": %d}`, 10*time.Second)
	req := httptest.NewRequest(http.MethodPatch, "/swarm/dispatcher", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	rec := httptest.NewRecorder()

	h.HandlePatchSwarmDispatcher(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	var disp swarm.DispatcherConfig
	if err := json.Unmarshal(resp["dispatcher"], &disp); err != nil {
		t.Fatal(err)
	}
	if disp.HeartbeatPeriod != 10*time.Second {
		t.Fatalf("HeartbeatPeriod=%v, want %v", disp.HeartbeatPeriod, 10*time.Second)
	}
}

func TestHandleSwarmDispatcher_NilClient(t *testing.T) {
	h := newSwarmHandlers(t, nil)
	req := httptest.NewRequest(http.MethodPatch, "/swarm/dispatcher", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	rec := httptest.NewRecorder()

	h.HandlePatchSwarmDispatcher(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestHandleSwarmDispatcher_WrongContentType(t *testing.T) {
	h := newSwarmHandlers(t, &mockSystemClient{})
	req := httptest.NewRequest(http.MethodPatch, "/swarm/dispatcher", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandlePatchSwarmDispatcher(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusUnsupportedMediaType)
	}
}

func TestHandleSwarmDispatcher_InspectError(t *testing.T) {
	sc := &mockSystemClient{swarmInspectFn: inspectError()}
	h := newSwarmHandlers(t, sc)

	rec := httptest.NewRecorder()
	h.HandlePatchSwarmDispatcher(rec, mergePatchRequest("/swarm/dispatcher", `{}`))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleSwarmDispatcher_UpdateError(t *testing.T) {
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: updateError(),
	}
	h := newSwarmHandlers(t, sc)

	rec := httptest.NewRecorder()
	h.HandlePatchSwarmDispatcher(
		rec,
		mergePatchRequest(
			"/swarm/dispatcher",
			fmt.Sprintf(`{"HeartbeatPeriod": %d}`, 10*time.Second),
		),
	)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestHandleSwarmDispatcher_InvalidBody(t *testing.T) {
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
	}
	h := newSwarmHandlers(t, sc)

	rec := httptest.NewRecorder()
	h.HandlePatchSwarmDispatcher(rec, mergePatchRequest("/swarm/dispatcher", "not json"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleSwarmDispatcher_EmptyPatch_PreservesHeartbeat(t *testing.T) {
	var capturedSpec swarm.Spec
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error {
			capturedSpec = spec
			return nil
		},
	}
	h := newSwarmHandlers(t, sc)

	rec := httptest.NewRecorder()
	h.HandlePatchSwarmDispatcher(rec, mergePatchRequest("/swarm/dispatcher", `{}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusOK)
	}
	if capturedSpec.Dispatcher.HeartbeatPeriod != 5*time.Second {
		t.Fatalf(
			"HeartbeatPeriod=%v, want %v (preserved)",
			capturedSpec.Dispatcher.HeartbeatPeriod,
			5*time.Second,
		)
	}
}

// --- HandlePatchSwarmCAConfig ---

func TestHandleSwarmCAConfig_Success(t *testing.T) {
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: noopUpdate(),
	}
	h := newSwarmHandlers(t, sc)

	newExpiry := 30 * 24 * time.Hour
	body := fmt.Sprintf(`{"NodeCertExpiry": %d}`, newExpiry)
	req := httptest.NewRequest(http.MethodPatch, "/swarm/ca", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	rec := httptest.NewRecorder()

	h.HandlePatchSwarmCAConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	var ca swarm.CAConfig
	if err := json.Unmarshal(resp["caConfig"], &ca); err != nil {
		t.Fatal(err)
	}
	if ca.NodeCertExpiry != newExpiry {
		t.Fatalf("NodeCertExpiry=%v, want %v", ca.NodeCertExpiry, newExpiry)
	}
}

func TestHandleSwarmCAConfig_NilClient(t *testing.T) {
	h := newSwarmHandlers(t, nil)
	req := httptest.NewRequest(http.MethodPatch, "/swarm/ca", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	rec := httptest.NewRecorder()

	h.HandlePatchSwarmCAConfig(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestHandleSwarmCAConfig_WrongContentType(t *testing.T) {
	h := newSwarmHandlers(t, &mockSystemClient{})
	req := httptest.NewRequest(http.MethodPatch, "/swarm/ca", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandlePatchSwarmCAConfig(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusUnsupportedMediaType)
	}
}

func TestHandleSwarmCAConfig_InspectError(t *testing.T) {
	sc := &mockSystemClient{swarmInspectFn: inspectError()}
	h := newSwarmHandlers(t, sc)

	rec := httptest.NewRecorder()
	h.HandlePatchSwarmCAConfig(rec, mergePatchRequest("/swarm/ca", `{}`))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleSwarmCAConfig_UpdateError(t *testing.T) {
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: updateError(),
	}
	h := newSwarmHandlers(t, sc)

	newExpiry := 30 * 24 * time.Hour
	rec := httptest.NewRecorder()
	h.HandlePatchSwarmCAConfig(
		rec,
		mergePatchRequest("/swarm/ca", fmt.Sprintf(`{"NodeCertExpiry": %d}`, newExpiry)),
	)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestHandleSwarmCAConfig_InvalidBody(t *testing.T) {
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
	}
	h := newSwarmHandlers(t, sc)

	rec := httptest.NewRecorder()
	h.HandlePatchSwarmCAConfig(rec, mergePatchRequest("/swarm/ca", "not json"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleSwarmCAConfig_EmptyPatch_PreservesExpiry(t *testing.T) {
	var capturedSpec swarm.Spec
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error {
			capturedSpec = spec
			return nil
		},
	}
	h := newSwarmHandlers(t, sc)

	rec := httptest.NewRecorder()
	h.HandlePatchSwarmCAConfig(rec, mergePatchRequest("/swarm/ca", `{}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusOK)
	}
	if capturedSpec.CAConfig.NodeCertExpiry != 90*24*time.Hour {
		t.Fatalf(
			"NodeCertExpiry=%v, want %v (preserved)",
			capturedSpec.CAConfig.NodeCertExpiry,
			90*24*time.Hour,
		)
	}
}

// --- HandlePatchSwarmEncryption ---

func TestHandleSwarmEncryption_Success(t *testing.T) {
	var capturedSpec swarm.Spec
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error {
			capturedSpec = spec
			return nil
		},
	}
	h := newSwarmHandlers(t, sc)

	body := `{"AutoLockManagers": true}`
	req := httptest.NewRequest(http.MethodPatch, "/swarm/encryption", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	rec := httptest.NewRecorder()

	h.HandlePatchSwarmEncryption(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusOK)
	}

	if !capturedSpec.EncryptionConfig.AutoLockManagers {
		t.Fatal("AutoLockManagers should be true")
	}
}

func TestHandleSwarmEncryption_SetFalse(t *testing.T) {
	sw := validSwarm()
	sw.Spec.EncryptionConfig.AutoLockManagers = true

	var capturedSpec swarm.Spec
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return sw, nil
		},
		updateSwarmFn: func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error {
			capturedSpec = spec
			return nil
		},
	}
	h := newSwarmHandlers(t, sc)

	body := `{"AutoLockManagers": false}`
	req := httptest.NewRequest(http.MethodPatch, "/swarm/encryption", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	rec := httptest.NewRecorder()

	h.HandlePatchSwarmEncryption(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusOK)
	}

	if capturedSpec.EncryptionConfig.AutoLockManagers {
		t.Fatal("AutoLockManagers should be false")
	}
}

func TestHandleSwarmEncryption_NilClient(t *testing.T) {
	h := newSwarmHandlers(t, nil)
	req := httptest.NewRequest(http.MethodPatch, "/swarm/encryption", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	rec := httptest.NewRecorder()

	h.HandlePatchSwarmEncryption(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestHandleSwarmEncryption_WrongContentType(t *testing.T) {
	h := newSwarmHandlers(t, &mockSystemClient{})
	req := httptest.NewRequest(http.MethodPatch, "/swarm/encryption", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandlePatchSwarmEncryption(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusUnsupportedMediaType)
	}
}

func TestHandleSwarmEncryption_InspectError(t *testing.T) {
	sc := &mockSystemClient{swarmInspectFn: inspectError()}
	h := newSwarmHandlers(t, sc)

	rec := httptest.NewRecorder()
	h.HandlePatchSwarmEncryption(rec, mergePatchRequest("/swarm/encryption", `{}`))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleSwarmEncryption_UpdateError(t *testing.T) {
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: updateError(),
	}
	h := newSwarmHandlers(t, sc)

	rec := httptest.NewRecorder()
	h.HandlePatchSwarmEncryption(
		rec,
		mergePatchRequest("/swarm/encryption", `{"AutoLockManagers": true}`),
	)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestHandleSwarmEncryption_InvalidBody(t *testing.T) {
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
	}
	h := newSwarmHandlers(t, sc)

	rec := httptest.NewRecorder()
	h.HandlePatchSwarmEncryption(rec, mergePatchRequest("/swarm/encryption", "not json"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleSwarmEncryption_OmittedField_NoChange(t *testing.T) {
	sw := validSwarm()
	sw.Spec.EncryptionConfig.AutoLockManagers = true

	var capturedSpec swarm.Spec
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return sw, nil
		},
		updateSwarmFn: func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error {
			capturedSpec = spec
			return nil
		},
	}
	h := newSwarmHandlers(t, sc)

	// Empty patch: AutoLockManagers pointer is nil, so the field should not change
	rec := httptest.NewRecorder()
	h.HandlePatchSwarmEncryption(rec, mergePatchRequest("/swarm/encryption", `{}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusOK)
	}
	if !capturedSpec.EncryptionConfig.AutoLockManagers {
		t.Fatal("AutoLockManagers should remain true when omitted from patch")
	}
}

// --- HandlePostRotateToken ---

func TestHandlePostRotateToken_Worker(t *testing.T) {
	var capturedFlags swarm.UpdateFlags
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error {
			capturedFlags = flags
			return nil
		},
	}
	h := newSwarmHandlers(t, sc)

	body := `{"target":"worker"}`
	req := httptest.NewRequest(http.MethodPost, "/swarm/rotate-token", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandlePostRotateToken(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusNoContent)
	}
	if !capturedFlags.RotateWorkerToken {
		t.Fatal("RotateWorkerToken should be true")
	}
	if capturedFlags.RotateManagerToken {
		t.Fatal("RotateManagerToken should be false")
	}
}

func TestHandlePostRotateToken_Manager(t *testing.T) {
	var capturedFlags swarm.UpdateFlags
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error {
			capturedFlags = flags
			return nil
		},
	}
	h := newSwarmHandlers(t, sc)

	body := `{"target":"manager"}`
	req := httptest.NewRequest(http.MethodPost, "/swarm/rotate-token", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandlePostRotateToken(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusNoContent)
	}
	if !capturedFlags.RotateManagerToken {
		t.Fatal("RotateManagerToken should be true")
	}
}

func TestHandlePostRotateToken_InvalidTarget(t *testing.T) {
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
	}
	h := newSwarmHandlers(t, sc)

	body := `{"target":"invalid"}`
	req := httptest.NewRequest(http.MethodPost, "/swarm/rotate-token", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandlePostRotateToken(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandlePostRotateToken_NilClient(t *testing.T) {
	h := newSwarmHandlers(t, nil)
	req := httptest.NewRequest(
		http.MethodPost,
		"/swarm/rotate-token",
		strings.NewReader(`{"target":"worker"}`),
	)
	rec := httptest.NewRecorder()

	h.HandlePostRotateToken(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestHandlePostRotateToken_InspectError(t *testing.T) {
	sc := &mockSystemClient{
		swarmInspectFn: inspectError(),
	}
	h := newSwarmHandlers(t, sc)

	body := `{"target":"worker"}`
	req := httptest.NewRequest(http.MethodPost, "/swarm/rotate-token", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandlePostRotateToken(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandlePostRotateToken_UpdateError(t *testing.T) {
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: updateError(),
	}
	h := newSwarmHandlers(t, sc)

	body := `{"target":"worker"}`
	req := httptest.NewRequest(http.MethodPost, "/swarm/rotate-token", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandlePostRotateToken(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestHandlePostRotateToken_InvalidBody(t *testing.T) {
	sc := &mockSystemClient{}
	h := newSwarmHandlers(t, sc)

	req := httptest.NewRequest(
		http.MethodPost,
		"/swarm/rotate-token",
		strings.NewReader("not json"),
	)
	rec := httptest.NewRecorder()

	h.HandlePostRotateToken(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandlePostRotateToken_EmptyTarget(t *testing.T) {
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
	}
	h := newSwarmHandlers(t, sc)

	body := `{"target":""}`
	req := httptest.NewRequest(http.MethodPost, "/swarm/rotate-token", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandlePostRotateToken(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// --- HandlePostRotateUnlockKey ---

func TestHandlePostRotateUnlockKey_Success(t *testing.T) {
	var capturedFlags swarm.UpdateFlags
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error {
			capturedFlags = flags
			return nil
		},
	}
	h := newSwarmHandlers(t, sc)

	req := httptest.NewRequest(http.MethodPost, "/swarm/rotate-unlock-key", nil)
	rec := httptest.NewRecorder()

	h.HandlePostRotateUnlockKey(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusNoContent)
	}
	if !capturedFlags.RotateManagerUnlockKey {
		t.Fatal("RotateManagerUnlockKey should be true")
	}
}

func TestHandlePostRotateUnlockKey_NilClient(t *testing.T) {
	h := newSwarmHandlers(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/swarm/rotate-unlock-key", nil)
	rec := httptest.NewRecorder()

	h.HandlePostRotateUnlockKey(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestHandlePostRotateUnlockKey_InspectError(t *testing.T) {
	sc := &mockSystemClient{swarmInspectFn: inspectError()}
	h := newSwarmHandlers(t, sc)

	req := httptest.NewRequest(http.MethodPost, "/swarm/rotate-unlock-key", nil)
	rec := httptest.NewRecorder()

	h.HandlePostRotateUnlockKey(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandlePostRotateUnlockKey_UpdateError(t *testing.T) {
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: updateError(),
	}
	h := newSwarmHandlers(t, sc)

	req := httptest.NewRequest(http.MethodPost, "/swarm/rotate-unlock-key", nil)
	rec := httptest.NewRecorder()

	h.HandlePostRotateUnlockKey(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// --- HandlePostForceRotateCA ---

func TestHandlePostForceRotateCA_Success(t *testing.T) {
	var capturedSpec swarm.Spec
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error {
			capturedSpec = spec
			return nil
		},
	}
	h := newSwarmHandlers(t, sc)

	req := httptest.NewRequest(http.MethodPost, "/swarm/force-rotate-ca", nil)
	rec := httptest.NewRecorder()

	h.HandlePostForceRotateCA(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want %d; body: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	// ForceRotate should be incremented from 0 to 1
	if capturedSpec.CAConfig.ForceRotate != 1 {
		t.Fatalf("ForceRotate=%d, want 1", capturedSpec.CAConfig.ForceRotate)
	}
}

func TestHandlePostForceRotateCA_IncrementsExisting(t *testing.T) {
	sw := validSwarm()
	sw.Spec.CAConfig.ForceRotate = 5

	var capturedSpec swarm.Spec
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return sw, nil
		},
		updateSwarmFn: func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error {
			capturedSpec = spec
			return nil
		},
	}
	h := newSwarmHandlers(t, sc)

	req := httptest.NewRequest(http.MethodPost, "/swarm/force-rotate-ca", nil)
	rec := httptest.NewRecorder()

	h.HandlePostForceRotateCA(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusNoContent)
	}
	if capturedSpec.CAConfig.ForceRotate != 6 {
		t.Fatalf("ForceRotate=%d, want 6 (5+1)", capturedSpec.CAConfig.ForceRotate)
	}
}

func TestHandlePostForceRotateCA_NilClient(t *testing.T) {
	h := newSwarmHandlers(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/swarm/force-rotate-ca", nil)
	rec := httptest.NewRecorder()

	h.HandlePostForceRotateCA(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestHandlePostForceRotateCA_InspectError(t *testing.T) {
	sc := &mockSystemClient{swarmInspectFn: inspectError()}
	h := newSwarmHandlers(t, sc)

	req := httptest.NewRequest(http.MethodPost, "/swarm/force-rotate-ca", nil)
	rec := httptest.NewRecorder()

	h.HandlePostForceRotateCA(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandlePostForceRotateCA_UpdateError(t *testing.T) {
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: updateError(),
	}
	h := newSwarmHandlers(t, sc)

	req := httptest.NewRequest(http.MethodPost, "/swarm/force-rotate-ca", nil)
	rec := httptest.NewRecorder()

	h.HandlePostForceRotateCA(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestHandlePostForceRotateCA_PassesVersion(t *testing.T) {
	var capturedVersion swarm.Version
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error {
			capturedVersion = version
			return nil
		},
	}
	h := newSwarmHandlers(t, sc)

	req := httptest.NewRequest(http.MethodPost, "/swarm/force-rotate-ca", nil)
	rec := httptest.NewRecorder()

	h.HandlePostForceRotateCA(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusNoContent)
	}
	if capturedVersion.Index != 42 {
		t.Fatalf("version.Index=%d, want 42", capturedVersion.Index)
	}
}

func TestHandlePostForceRotateCA_NoUpdateFlags(t *testing.T) {
	var capturedFlags swarm.UpdateFlags
	sc := &mockSystemClient{
		swarmInspectFn: func(ctx context.Context) (swarm.Swarm, error) {
			return validSwarm(), nil
		},
		updateSwarmFn: func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error {
			capturedFlags = flags
			return nil
		},
	}
	h := newSwarmHandlers(t, sc)

	req := httptest.NewRequest(http.MethodPost, "/swarm/force-rotate-ca", nil)
	rec := httptest.NewRecorder()

	h.HandlePostForceRotateCA(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusNoContent)
	}
	if capturedFlags.RotateWorkerToken || capturedFlags.RotateManagerToken ||
		capturedFlags.RotateManagerUnlockKey {
		t.Fatal("no rotation flags should be set for CA force-rotate")
	}
}

// --- HandleGetUnlockKey ---

func TestHandleGetUnlockKey_Success(t *testing.T) {
	sc := &mockSystemClient{
		getUnlockKeyFn: func(ctx context.Context) (string, error) {
			return "SWMKEY-1-abc123", nil
		},
	}
	h := newSwarmHandlers(t, sc)

	req := httptest.NewRequest(http.MethodGet, "/swarm/unlock-key", nil)
	rec := httptest.NewRecorder()

	h.HandleGetUnlockKey(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["unlockKey"] != "SWMKEY-1-abc123" {
		t.Fatalf("unlockKey=%q, want SWMKEY-1-abc123", resp["unlockKey"])
	}
}

func TestHandleGetUnlockKey_NilClient(t *testing.T) {
	h := newSwarmHandlers(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/swarm/unlock-key", nil)
	rec := httptest.NewRecorder()

	h.HandleGetUnlockKey(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestHandleGetUnlockKey_Error(t *testing.T) {
	sc := &mockSystemClient{
		getUnlockKeyFn: func(ctx context.Context) (string, error) {
			return "", fmt.Errorf("not a manager")
		},
	}
	h := newSwarmHandlers(t, sc)

	req := httptest.NewRequest(http.MethodGet, "/swarm/unlock-key", nil)
	rec := httptest.NewRecorder()

	h.HandleGetUnlockKey(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// --- HandlePostUnlockSwarm ---

func TestHandlePostUnlockSwarm_Success(t *testing.T) {
	var capturedKey string
	sc := &mockSystemClient{
		unlockSwarmFn: func(ctx context.Context, key string) error {
			capturedKey = key
			return nil
		},
	}
	h := newSwarmHandlers(t, sc)

	body := `{"unlockKey":"SWMKEY-1-abc123"}`
	req := httptest.NewRequest(http.MethodPost, "/swarm/unlock", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandlePostUnlockSwarm(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusNoContent)
	}
	if capturedKey != "SWMKEY-1-abc123" {
		t.Fatalf("key=%q, want SWMKEY-1-abc123", capturedKey)
	}
}

func TestHandlePostUnlockSwarm_MissingKey(t *testing.T) {
	sc := &mockSystemClient{}
	h := newSwarmHandlers(t, sc)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/swarm/unlock", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandlePostUnlockSwarm(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandlePostUnlockSwarm_InvalidBody(t *testing.T) {
	sc := &mockSystemClient{}
	h := newSwarmHandlers(t, sc)

	req := httptest.NewRequest(http.MethodPost, "/swarm/unlock", strings.NewReader("not json"))
	rec := httptest.NewRecorder()

	h.HandlePostUnlockSwarm(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandlePostUnlockSwarm_NilClient(t *testing.T) {
	h := newSwarmHandlers(t, nil)

	body := `{"unlockKey":"SWMKEY-1-abc123"}`
	req := httptest.NewRequest(http.MethodPost, "/swarm/unlock", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandlePostUnlockSwarm(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestHandlePostUnlockSwarm_Error(t *testing.T) {
	sc := &mockSystemClient{
		unlockSwarmFn: func(ctx context.Context, key string) error {
			return fmt.Errorf("invalid key")
		},
	}
	h := newSwarmHandlers(t, sc)

	body := `{"unlockKey":"SWMKEY-1-wrong"}`
	req := httptest.NewRequest(http.MethodPost, "/swarm/unlock", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandlePostUnlockSwarm(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
