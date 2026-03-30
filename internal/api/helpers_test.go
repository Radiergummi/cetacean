package api

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/api/prometheus"
	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

type testHandlersConfig struct {
	cache           *cache.Cache
	broadcaster     *sse.Broadcaster
	dockerClient    DockerLogStreamer
	systemClient    DockerSystemClient
	writeClient     DockerWriteClient
	pluginClient    DockerPluginClient
	ready           <-chan struct{}
	promClient      *prometheus.Client
	operationsLevel config.OperationsLevel
}

type testHandlersOption func(*testHandlersConfig)

func withCache(c *cache.Cache) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.cache = c }
}

func withWriteClient(wc DockerWriteClient) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.writeClient = wc }
}

func withOpsLevel(level config.OperationsLevel) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.operationsLevel = level }
}

func withPromClient(pc *prometheus.Client) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.promClient = pc }
}

func withReady(ch <-chan struct{}) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.ready = ch }
}

func withPluginClient(pc DockerPluginClient) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.pluginClient = pc }
}

func withSystemClient(sc DockerSystemClient) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.systemClient = sc }
}

func withDockerClient(dc DockerLogStreamer) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.dockerClient = dc }
}

func withBroadcaster(b *sse.Broadcaster) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.broadcaster = b }
}

// newTestHandlers creates a Handlers instance with sensible test defaults.
// All dependencies default to nil except cache (empty), ready (closed), and
// operationsLevel (OpsImpactful). Use option functions to override.
func newTestHandlers(t testing.TB, opts ...testHandlersOption) *Handlers {
	t.Helper()

	cfg := testHandlersConfig{
		cache:           cache.New(nil),
		ready:           closedReady(),
		operationsLevel: config.OpsImpactful,
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	return NewHandlers(
		cfg.cache,
		cfg.broadcaster,
		cfg.dockerClient,
		cfg.systemClient,
		cfg.writeClient,
		cfg.pluginClient,
		cfg.ready,
		cfg.promClient,
		cfg.operationsLevel,
		nil, // recEngine
	)
}
