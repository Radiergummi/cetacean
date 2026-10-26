package mcp

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/docker"
)

// conditionalCase is one editing tool: the arguments that reach its write,
// the kind of record it edits, and how to route that write to a stub.
type conditionalCase struct {
	tool string
	kind string
	args map[string]any
	stub func(f *fakeWriteClient, write func(ctx context.Context, id string) error)
}

func serviceStub(
	set func(f *fakeWriteClient, write func(context.Context, string) (swarm.Service, error)),
) func(*fakeWriteClient, func(context.Context, string) error) {
	return func(f *fakeWriteClient, write func(context.Context, string) error) {
		set(f, func(ctx context.Context, id string) (swarm.Service, error) {
			return swarm.Service{ID: "svc1", Version: swarm.Version{Index: 42}},
				write(ctx, id)
		})
	}
}

func nodeStub(
	set func(f *fakeWriteClient, write func(context.Context, string) (swarm.Node, error)),
) func(*fakeWriteClient, func(context.Context, string) error) {
	return func(f *fakeWriteClient, write func(context.Context, string) error) {
		set(f, func(ctx context.Context, id string) (swarm.Node, error) {
			return swarm.Node{ID: "node1", Version: swarm.Version{Index: 10}},
				write(ctx, id)
		})
	}
}

// conditionalCases covers every tool that edits an existing record. The
// versions passed are the ones conditionalTestCache holds.
func conditionalCases() []conditionalCase {
	type svcWrite = func(context.Context, string) (swarm.Service, error)
	type nodeWrite = func(context.Context, string) (swarm.Node, error)

	return []conditionalCase{
		{"scale_service", "service", map[string]any{"id": "web", "replicas": float64(2)},
			serviceStub(func(f *fakeWriteClient, w svcWrite) {
				f.scaleServiceFn = func(ctx context.Context, id string, _ uint64) (swarm.Service, error) {
					return w(ctx, id)
				}
			})},
		{"update_service_image", "service", map[string]any{"id": "web", "image": "nginx:1"},
			serviceStub(func(f *fakeWriteClient, w svcWrite) {
				f.updateServiceImageFn = func(ctx context.Context, id, _ string) (swarm.Service, error) {
					return w(ctx, id)
				}
			})},
		{"rollback_service", "service", map[string]any{"id": "web"},
			serviceStub(func(f *fakeWriteClient, w svcWrite) { f.rollbackServiceFn = w })},
		{"restart_service", "service", map[string]any{"id": "web"},
			serviceStub(func(f *fakeWriteClient, w svcWrite) { f.restartServiceFn = w })},
		{"update_service", "service", map[string]any{
			"id": "web", "section": "healthcheck", "value": map[string]any{"test": []any{}},
		}, serviceStub(func(f *fakeWriteClient, w svcWrite) {
			f.updateServiceHealthFn = func(
				ctx context.Context, id string, _ *container.HealthConfig,
			) (swarm.Service, error) {
				return w(ctx, id)
			}
		})},
		{"update_service_secrets", "service", map[string]any{
			"id": "web", "secrets": []any{map[string]any{"name": "db_password_v2"}},
		}, serviceStub(func(f *fakeWriteClient, w svcWrite) {
			f.updateServiceSecretsFn = func(
				ctx context.Context, id string, _ []*swarm.SecretReference,
			) (swarm.Service, error) {
				return w(ctx, id)
			}
		})},
		{"update_service_configs", "service", map[string]any{
			"id": "web", "configs": []any{map[string]any{"name": "nginx_conf"}},
		}, serviceStub(func(f *fakeWriteClient, w svcWrite) {
			f.updateServiceConfigsFn = func(
				ctx context.Context, id string, _ []*swarm.ConfigReference,
			) (swarm.Service, error) {
				return w(ctx, id)
			}
		})},
		{"update_service_mounts", "service", map[string]any{
			"id": "web", "mounts": []any{
				map[string]any{"type": "volume", "source": "data", "target": "/data"},
			},
		}, serviceStub(func(f *fakeWriteClient, w svcWrite) {
			f.updateServiceMountsFn = func(
				ctx context.Context, id string, _ []mount.Mount,
			) (swarm.Service, error) {
				return w(ctx, id)
			}
		})},
		{"update_node_labels", "node", map[string]any{
			"id": "worker-1", "labels": map[string]any{"zone": "a"},
		}, nodeStub(func(f *fakeWriteClient, w nodeWrite) {
			f.updateNodeLabelsFn = func(
				ctx context.Context, id string, _ map[string]string,
			) (swarm.Node, error) {
				return w(ctx, id)
			}
		})},
		{"update_node", "node", map[string]any{
			"id": "worker-1", "section": "availability", "value": "drain",
		}, nodeStub(func(f *fakeWriteClient, w nodeWrite) {
			f.updateNodeAvailFn = func(
				ctx context.Context, id string, _ swarm.NodeAvailability,
			) (swarm.Node, error) {
				return w(ctx, id)
			}
		})},
	}
}

// checkedTools are the editing tools whose check reads the cached service and
// pins the write to what it read, whether or not a version was passed.
var checkedTools = []string{
	"update_service_secrets",
	"update_service_configs",
	"update_service_mounts",
}

func conditionalTestCache(t *testing.T) *cache.Cache {
	t.Helper()

	c := attachmentTestCache(t)
	c.SetNode(swarm.Node{
		ID:          "node1",
		Version:     swarm.Version{Index: 9},
		Description: swarm.NodeDescription{Hostname: "worker-1"},
	})

	return c
}

// cachedIndex is the version conditionalTestCache holds for the kind.
func cachedIndex(kind string) float64 {
	if kind == "node" {
		return 9
	}

	return 41
}

func resolvedID(kind string) string {
	if kind == "node" {
		return "node1"
	}

	return "svc1"
}

func callConditional(
	t *testing.T,
	tc conditionalCase,
	f *fakeWriteClient,
	version any,
) error {
	t.Helper()

	srv := newToolTestServer(t, conditionalTestCache(t), f, config.OpsImpactful)

	td, ok := srv.findTool(tc.tool)
	if !ok {
		t.Fatalf("%s is not registered", tc.tool)
	}

	args := maps.Clone(tc.args)
	if version != nil {
		args["version"] = version
	}

	_, err := td.handler(context.Background(), newCallToolRequest(tc.tool, args))

	return err
}

// Every tool editing an existing record takes `version`, optionally, and no
// other tool does: a removal names no version to the engine, and a create
// has none to name.
func TestEditingToolsTakeAnOptionalVersion(t *testing.T) {
	srv := newToolTestServer(t, cache.New(nil), &fakeWriteClient{}, config.OpsImpactful)

	editing := map[string]string{}
	for _, tc := range conditionalCases() {
		editing[tc.tool] = tc.kind
	}

	for _, td := range srv.toolCatalog() {
		name := td.tool.Name
		_, has := td.tool.InputSchema.Properties["version"]
		kind, edits := editing[name]

		if has != edits {
			t.Errorf("%s: takes version = %v, want %v", name, has, edits)
		}

		if edits && !cluster.Versioned(kind) {
			t.Errorf("%s edits a %s, which cluster.Versioned says takes no version", name, kind)
		}

		if slices.Contains(td.tool.InputSchema.Required, "version") {
			t.Errorf("%s requires version; it must stay optional", name)
		}

		annotations := td.tool.Annotations
		writes := annotations.ReadOnlyHint != nil && !*annotations.ReadOnlyHint
		if writes && !edits &&
			!strings.HasPrefix(name, "remove_") && !strings.HasPrefix(name, "create_") {
			t.Errorf("%s writes but is neither a removal, a create nor a conditional edit", name)
		}
	}
}

// The write is pinned to the caller's version, keyed by the resolved ID the
// writer is handed, so a call by name is as conditional as one by ID.
func TestAVersionPinsTheWriteToIt(t *testing.T) {
	for _, tc := range conditionalCases() {
		t.Run(tc.tool, func(t *testing.T) {
			var (
				pinned swarm.Version
				ok     bool
				gotID  string
			)

			f := &fakeWriteClient{}
			tc.stub(f, func(ctx context.Context, id string) error {
				gotID = id
				pinned, ok = docker.PinnedVersion(ctx, tc.kind, id)

				return nil
			})

			if err := callConditional(t, tc, f, cachedIndex(tc.kind)); err != nil {
				t.Fatalf("call: %v", err)
			}

			if gotID != resolvedID(tc.kind) {
				t.Errorf("writer got %q, want the resolved %q", gotID, resolvedID(tc.kind))
			}

			if !ok || float64(pinned.Index) != cachedIndex(tc.kind) {
				t.Errorf("pinned = %d (ok=%v), want %v", pinned.Index, ok, cachedIndex(tc.kind))
			}
		})
	}
}

func TestNoVersionLeavesTheWriteUnpinned(t *testing.T) {
	for _, tc := range conditionalCases() {
		if slices.Contains(checkedTools, tc.tool) {
			continue
		}

		t.Run(tc.tool, func(t *testing.T) {
			f := &fakeWriteClient{}
			tc.stub(f, func(ctx context.Context, id string) error {
				if v, ok := docker.PinnedVersion(ctx, tc.kind, id); ok {
					t.Errorf("pinned at %d without a version argument", v.Index)
				}

				return nil
			})

			if err := callConditional(t, tc, f, nil); err != nil {
				t.Fatalf("call: %v", err)
			}
		})
	}
}

// A stale version is the MCP counterpart of 412: refused, saying so, naming
// the version to read again at.
func TestAStaleVersionIsRefusedNamingTheCurrentOne(t *testing.T) {
	for _, tc := range conditionalCases() {
		t.Run(tc.tool, func(t *testing.T) {
			f := &fakeWriteClient{}
			tc.stub(f, func(_ context.Context, id string) error {
				return &docker.StaleVersionError{
					Kind:    tc.kind,
					ID:      id,
					Pinned:  uint64(cachedIndex(tc.kind)),
					Current: 77,
				}
			})

			err := callConditional(t, tc, f, cachedIndex(tc.kind))
			if err == nil {
				t.Fatal("a stale version was accepted")
			}

			for _, want := range []string{"changed since you read it", "77", "Nothing was written"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not say %q", err, want)
				}
			}
		})
	}
}

// The engine's own refusal, for a record that moved between the writer's read
// and its write, is the same answer.
func TestTheEnginesSequenceConflictIsAlsoAStaleVersion(t *testing.T) {
	tc := conditionalCases()[0]

	f := &fakeWriteClient{}
	tc.stub(f, func(context.Context, string) error {
		return errors.New("rpc error: code = Unknown desc = update out of sequence")
	})

	err := callConditional(t, tc, f, cachedIndex(tc.kind))
	if err == nil || !strings.Contains(err.Error(), "changed since you read it") {
		t.Errorf("err = %v, want it reported as a changed record", err)
	}
}

// A tool whose check reads the cached service must be pinned to what the
// check read; a caller's version that differs from it cannot be honoured
// without writing against a spec the check never saw.
func TestACheckedWriteRefusesAVersionItsCheckDidNotRead(t *testing.T) {
	for _, tc := range conditionalCases() {
		if !slices.Contains(checkedTools, tc.tool) {
			continue
		}

		t.Run(tc.tool, func(t *testing.T) {
			for _, version := range []float64{40, 43} {
				err := callConditional(t, tc, &fakeWriteClient{}, version)
				if err == nil {
					t.Fatalf("version %v accepted against a check that read 41", version)
				}

				if strings.Contains(err.Error(), "not stubbed") {
					t.Errorf("version %v reached the writer: %v", version, err)
				}
			}
		})
	}
}

func TestVersionMustBeAVersionIndex(t *testing.T) {
	tc := conditionalCases()[0]

	for _, version := range []any{float64(-1), 1.5, "41", true} {
		err := callConditional(t, tc, &fakeWriteClient{}, version)
		if err == nil || strings.Contains(err.Error(), "not stubbed") {
			t.Errorf("version %#v: err = %v, want a refusal before the write", version, err)
		}
	}
}

// A conditional write must address the record the version belongs to, so one
// the cache cannot resolve is refused rather than sent unpinned.
func TestAVersionOnAnUnknownRecordIsRefused(t *testing.T) {
	tc := conditionalCases()[0]
	tc.args = maps.Clone(tc.args)
	tc.args["id"] = "nope"

	err := callConditional(t, tc, &fakeWriteClient{}, float64(41))
	if err == nil || strings.Contains(err.Error(), "not stubbed") {
		t.Errorf("err = %v, want a refusal before the write", err)
	}
}
