package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/config"
)

type mockSystemClient struct {
	swarmInspectFn func(ctx context.Context) (swarm.Swarm, error)
	diskUsageFn    func(ctx context.Context) (types.DiskUsage, error)
	pluginListFn   func(ctx context.Context) (types.PluginsListResponse, error)
	localNodeIDFn  func(ctx context.Context) (string, error)
	updateSwarmFn  func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error
	getUnlockKeyFn func(ctx context.Context) (string, error)
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

func (m *mockSystemClient) PluginList(ctx context.Context) (types.PluginsListResponse, error) {
	if m.pluginListFn != nil {
		return m.pluginListFn(ctx)
	}
	return types.PluginsListResponse{}, fmt.Errorf("not implemented")
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

// Compile-time check: mockSystemClient must satisfy DockerSystemClient.
var _ DockerSystemClient = (*mockSystemClient)(nil)

// Silence unused import warnings for packages referenced only by future tests.
var (
	_ = http.StatusOK
	_ = httptest.NewRecorder
	_ = strings.NewReader
	_ = testing.T{}
	_ = json.Marshal
	_ = config.OperationsLevel(0)
)
