package cluster

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

type recordingWriter struct {
	called string
	ctx    context.Context
	err    error

	// live is the spec Docker holds, which can differ from the cache's.
	live    *swarm.ServiceSpec
	written swarm.ServiceSpec
}

func (w *recordingWriter) record(ctx context.Context, call string) (swarm.Service, error) {
	w.called, w.ctx = call, ctx

	return swarm.Service{ID: "svc-web"}, w.err
}

func (w *recordingWriter) ScaleService(
	ctx context.Context,
	_ string,
	_ uint64,
) (swarm.Service, error) {
	return w.record(ctx, "scale")
}

func (w *recordingWriter) UpdateServiceImage(
	ctx context.Context,
	_, _ string,
) (swarm.Service, error) {
	return w.record(ctx, "image")
}

func (w *recordingWriter) RollbackService(ctx context.Context, _ string) (swarm.Service, error) {
	return w.record(ctx, "rollback")
}

func (w *recordingWriter) RestartService(ctx context.Context, _ string) (swarm.Service, error) {
	return w.record(ctx, "restart")
}

func (w *recordingWriter) UpdateServiceSpec(
	ctx context.Context,
	_ string,
	change SpecChange,
) (swarm.Service, error) {
	spec := writable("web").Spec
	if w.live != nil {
		spec = *w.live
	}
	if err := change(&spec); err != nil {
		return swarm.Service{}, err
	}
	w.written = spec

	return w.record(ctx, "spec")
}

func everything(string) bool { return true }
func nothing(string) bool    { return false }

func operator(level config.OperationsLevel) Caller {
	return Caller{CanRead: everything, CanWrite: everything, Level: level}
}

func writesOver(t *testing.T, services ...swarm.Service) (*Writes, *recordingWriter) {
	t.Helper()

	c := cache.New(nil)
	for _, svc := range services {
		c.SetService(svc)
	}

	writer := &recordingWriter{}

	return NewWrites(c, writer), writer
}

func writable(name string) swarm.Service {
	one := uint64(1)

	return swarm.Service{
		ID:      "svc-" + name,
		Version: swarm.Version{Index: 7},
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: name},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1"},
			},
		},
	}
}

func kindOf(t *testing.T, err error) WriteErrorKind {
	t.Helper()

	refusal, ok := errors.AsType[*WriteError](err)
	if !ok {
		t.Fatalf("error = %v (%T), want a *WriteError", err, err)
	}

	return refusal.Kind
}

func TestWritesResolvesTheTargetForTheCaller(t *testing.T) {
	readOnly := Caller{CanRead: everything, CanWrite: nothing, Level: config.OpsImpactful}
	blind := Caller{CanRead: nothing, CanWrite: nothing, Level: config.OpsImpactful}

	for _, tc := range []struct {
		name   string
		caller Caller
		key    string
		want   WriteErrorKind
	}{
		{"absent", operator(config.OpsImpactful), "nope", NotFound},
		{"unreadable, so a refusal never confirms it exists", blind, "web", NotFound},
		{"readable but not writable", readOnly, "web", Denied},
		{"below the operation's tier", operator(config.OpsReadOnly), "web", BelowTier},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes, writer := writesOver(t, writable("web"))

			_, err := writes.RestartService(t.Context(), tc.caller, tc.key)
			if got := kindOf(t, err); got != tc.want {
				t.Errorf("kind = %d, want %d", got, tc.want)
			}
			if writer.called != "" {
				t.Errorf("Docker was called (%s) for a refused write", writer.called)
			}
		})
	}
}

// Decided for both transports alike: refused from the cache, before Docker.
func TestWritesRefusesScalingAGlobalService(t *testing.T) {
	global := writable("agent")
	global.Spec.Mode = swarm.ServiceMode{Global: &swarm.GlobalService{}}
	writes, writer := writesOver(t, global)

	_, err := writes.ScaleService(t.Context(), operator(config.OpsImpactful), "agent", 3)

	if kindOf(t, err) != Invalid || !errors.Is(err, ErrScaleGlobal) {
		t.Errorf("error = %v, want Invalid wrapping ErrScaleGlobal", err)
	}
	if writer.called != "" {
		t.Error("Docker was called to scale a global service")
	}
}

func TestWritesRefusesRollingBackAServiceNeverUpdated(t *testing.T) {
	writes, _ := writesOver(t, writable("web"))

	_, err := writes.RollbackService(t.Context(), operator(config.OpsImpactful), "web")

	if kindOf(t, err) != Invalid || !errors.Is(err, ErrNoPreviousSpec) {
		t.Errorf("error = %v, want Invalid wrapping ErrNoPreviousSpec", err)
	}
}

func TestWritesAddressesDockerByID(t *testing.T) {
	writes, writer := writesOver(t, writable("web"))

	if _, err := writes.ScaleService(
		t.Context(),
		operator(config.OpsOperational),
		"web",
		2,
	); err != nil {
		t.Fatalf("ScaleService: %v", err)
	}
	if writer.called != "scale" {
		t.Errorf("called = %q, want scale", writer.called)
	}
}

func socketHolder() swarm.Service {
	svc := writable("proxy")
	svc.Spec.TaskTemplate.ContainerSpec.Mounts = []mount.Mount{{
		Type: mount.TypeBind, Source: "/var/run/docker.sock", Target: "/var/run/docker.sock",
	}}

	return svc
}

func TestWritesHoldsHostAccessBelowLevelThree(t *testing.T) {
	writes, writer := writesOver(t, socketHolder())

	_, err := writes.UpdateServiceImage(
		t.Context(), operator(config.OpsConfiguration), "proxy", "nginx:2",
	)
	if kindOf(t, err) != HostAccess {
		t.Errorf("error = %v, want HostAccess", err)
	}

	if _, err := writes.UpdateServiceImage(
		t.Context(), operator(config.OpsImpactful), "proxy", "nginx:2",
	); err != nil {
		t.Errorf("at level 3: %v", err)
	}
	if writer.called != "image" {
		t.Errorf("called = %q, want image at level 3", writer.called)
	}
}

func TestResolvableNamesWhatLookupFinds(t *testing.T) {
	c := cache.New(nil)
	web := writable("web")
	web.Spec.Labels = map[string]string{"com.docker.stack.namespace": "shop"}
	c.SetService(web)
	c.SetNode(swarm.Node{ID: "node-1"})
	c.SetTask(swarm.Task{ID: "task-1", ServiceID: web.ID})
	c.SetConfig(swarm.Config{ID: "cfg-1"})
	c.SetSecret(swarm.Secret{ID: "sec-1"})
	c.SetNetwork(network.Summary{ID: "net-1"})
	c.SetVolume(volume.Volume{Name: "data"})

	seeded := map[string]string{
		"service": web.ID, "node": "node-1", "task": "task-1", "config": "cfg-1",
		"secret": "sec-1", "network": "net-1", "stack": "shop", "volume": "data",
		"plugin": "web", "swarm": "web",
	}
	for kind, ident := range seeded {
		_, found, err := Lookup(c, kind, ident)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if Resolvable(kind) != found {
			t.Errorf("%s: Resolvable = %t, Lookup found = %t", kind, Resolvable(kind), found)
		}
	}
}

func TestUpdateServiceChecksTheLiveSpec(t *testing.T) {
	// The cache has caught up with neither: Docker's copy is what is judged.
	held := socketHolder()
	unheld := writable("proxy")

	for _, tc := range []struct {
		name         string
		cached, live swarm.Service
		want         WriteErrorKind
	}{
		{"live holds host access the cache does not show", unheld, held, HostAccess},
		{"cache holds host access the live spec dropped", held, unheld, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes, writer := writesOver(t, tc.cached)
			writer.live = &tc.live.Spec

			_, err := writes.UpdateService(
				t.Context(), operator(config.OpsConfiguration), OpUpdateService, "proxy",
				PatchEnv(func(map[string]string) (map[string]string, error) {
					return map[string]string{"MODE": "debug"}, nil
				}),
			)

			if tc.want == 0 {
				if err != nil {
					t.Fatalf("UpdateService: %v", err)
				}
				if got := writer.written.TaskTemplate.ContainerSpec.Env; len(got) != 1 {
					t.Errorf("env written = %v, want MODE=debug", got)
				}
				return
			}
			if got := kindOf(t, err); got != tc.want {
				t.Errorf("kind = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestUpdateServiceLetsAHolderChangeWhatDoesNotRun(t *testing.T) {
	holder := socketHolder()
	writes, writer := writesOver(t, holder)
	writer.live = &holder.Spec

	for _, change := range []SpecChange{
		SetResources(&swarm.ResourceRequirements{Limits: &swarm.Limit{MemoryBytes: 1 << 30}}),
		SetPlacement(nil),
	} {
		if _, err := writes.UpdateService(
			t.Context(), operator(config.OpsConfiguration), OpUpdateService, "proxy", change,
		); err != nil {
			t.Errorf("UpdateService: %v", err)
		}
	}
}

func TestUpdateServiceRefusesWhatTheCallerMayNotAttach(t *testing.T) {
	stack := map[string]string{StackNamespaceLabel: "billing"}

	for _, tc := range []struct {
		name   string
		change SpecChange
	}{
		{"an unreadable secret", SetSecrets([]*swarm.SecretReference{{
			SecretID: "sec-1", SecretName: "db-password",
		}})},
		{"an unreadable volume", SetMounts([]mount.Mount{{
			Type: mount.TypeVolume, Source: "data", Target: "/data",
		}})},
		{"a stack it may not write", PatchLabels(func(map[string]string) (map[string]string, error) {
			return stack, nil
		})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes, writer := writesOver(t, writable("web"))
			caller := Caller{
				CanRead:  func(resource string) bool { return resource == "service:web" },
				CanWrite: func(resource string) bool { return resource == "service:web" },
				Level:    config.OpsImpactful,
			}

			_, err := writes.UpdateService(t.Context(), caller, OpUpdateService, "web", tc.change)

			if kindOf(t, err) != Denied {
				t.Errorf("error = %v, want Denied", err)
			}
			if writer.called != "" {
				t.Error("the refused change was written")
			}
		})
	}
}

func TestUpdateServiceRefusesAContainerItDoesNotHave(t *testing.T) {
	writes, writer := writesOver(t, writable("web"))
	writer.live = &swarm.ServiceSpec{Name: "web"}

	_, err := writes.UpdateService(
		t.Context(), operator(config.OpsImpactful), OpUpdateService, "web",
		SetHealthcheck(nil),
	)

	if kindOf(t, err) != Invalid || !errors.Is(err, ErrNoContainerSpec) {
		t.Errorf("error = %v, want Invalid wrapping ErrNoContainerSpec", err)
	}
}
