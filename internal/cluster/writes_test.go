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
