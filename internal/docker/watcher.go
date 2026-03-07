package docker

import (
	"context"
	"io"
	"log"
	"sync"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"cetacean/internal/cache"
)

// DockerClient abstracts the Docker API methods used by the Watcher.
type DockerClient interface {
	ListNodes(ctx context.Context) ([]swarm.Node, error)
	ListServices(ctx context.Context) ([]swarm.Service, error)
	ListTasks(ctx context.Context) ([]swarm.Task, error)
	ListConfigs(ctx context.Context) ([]swarm.Config, error)
	ListSecrets(ctx context.Context) ([]swarm.Secret, error)
	ListNetworks(ctx context.Context) ([]network.Summary, error)
	ListVolumes(ctx context.Context) ([]volume.Volume, error)
	InspectNode(ctx context.Context, id string) (swarm.Node, error)
	InspectService(ctx context.Context, id string) (swarm.Service, error)
	InspectTask(ctx context.Context, id string) (swarm.Task, error)
	InspectConfig(ctx context.Context, id string) (swarm.Config, error)
	InspectSecret(ctx context.Context, id string) (swarm.Secret, error)
	InspectNetwork(ctx context.Context, id string) (network.Summary, error)
	InspectVolume(ctx context.Context, name string) (volume.Volume, error)
	Events(ctx context.Context) (<-chan events.Message, <-chan error)
	ServiceLogs(ctx context.Context, serviceID string, tail string, follow bool, since, until string) (io.ReadCloser, error)
	TaskLogs(ctx context.Context, taskID string, tail string, follow bool, since, until string) (io.ReadCloser, error)
	Close() error
}

type Watcher struct {
	client   DockerClient
	cache    *cache.Cache
	syncOnce sync.Once
	ready    chan struct{}
}

func NewWatcher(client DockerClient, cache *cache.Cache) *Watcher {
	return &Watcher{
		client: client,
		cache:  cache,
		ready:  make(chan struct{}),
	}
}

// Ready returns a channel that is closed after the first full sync completes.
func (w *Watcher) Ready() <-chan struct{} {
	return w.ready
}

// Run starts the watcher. It blocks until the context is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	w.fullSync(ctx)
	w.syncOnce.Do(func() { close(w.ready) })

	// Periodic re-sync safety net
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				log.Println("periodic full re-sync")
				w.fullSync(ctx)
			}
		}
	}()

	// Event stream with reconnect
	for {
		if ctx.Err() != nil {
			return
		}
		w.watchEvents(ctx)
		log.Println("event stream disconnected, reconnecting in 1s...")
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
		}
		log.Println("re-syncing after reconnect")
		w.fullSync(ctx)
	}
}

func (w *Watcher) fullSync(ctx context.Context) {
	log.Println("starting full sync")

	type result struct {
		name string
		err  error
	}
	ch := make(chan result, 7)

	go func() {
		nodes, err := w.client.ListNodes(ctx)
		if err == nil {
			w.cache.ReplaceNodes(nodes)
		}
		ch <- result{"nodes", err}
	}()

	go func() {
		services, err := w.client.ListServices(ctx)
		if err == nil {
			w.cache.ReplaceServices(services)
		}
		ch <- result{"services", err}
	}()

	go func() {
		tasks, err := w.client.ListTasks(ctx)
		if err == nil {
			w.cache.ReplaceTasks(tasks)
		}
		ch <- result{"tasks", err}
	}()

	go func() {
		configs, err := w.client.ListConfigs(ctx)
		if err == nil {
			w.cache.ReplaceConfigs(configs)
		}
		ch <- result{"configs", err}
	}()

	go func() {
		secrets, err := w.client.ListSecrets(ctx)
		if err == nil {
			w.cache.ReplaceSecrets(secrets)
		}
		ch <- result{"secrets", err}
	}()

	go func() {
		networks, err := w.client.ListNetworks(ctx)
		if err == nil {
			w.cache.ReplaceNetworks(networks)
		}
		ch <- result{"networks", err}
	}()

	go func() {
		volumes, err := w.client.ListVolumes(ctx)
		if err == nil {
			w.cache.ReplaceVolumes(volumes)
		}
		ch <- result{"volumes", err}
	}()

	for i := 0; i < 7; i++ {
		r := <-ch
		if r.err != nil {
			log.Printf("full sync %s failed: %v", r.name, r.err)
		}
	}

	snap := w.cache.Snapshot()
	log.Printf("full sync complete: %d nodes, %d services, %d tasks, %d stacks",
		snap.NodeCount, snap.ServiceCount, snap.TaskCount, snap.StackCount)
}

func (w *Watcher) watchEvents(ctx context.Context) {
	msgCh, errCh := w.client.Events(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-errCh:
			if err != nil {
				log.Printf("event stream error: %v", err)
			}
			return
		case msg := <-msgCh:
			w.handleEvent(ctx, msg)
		}
	}
}

func (w *Watcher) handleEvent(ctx context.Context, msg events.Message) {
	switch msg.Type {
	case events.NodeEventType:
		if msg.Action == "remove" {
			w.cache.DeleteNode(msg.Actor.ID)
		} else {
			node, err := w.client.InspectNode(ctx, msg.Actor.ID)
			if err != nil {
				log.Printf("inspect node %s failed: %v", msg.Actor.ID, err)
				return
			}
			w.cache.SetNode(node)
		}

	case events.ServiceEventType:
		if msg.Action == "remove" {
			w.cache.DeleteService(msg.Actor.ID)
		} else {
			svc, err := w.client.InspectService(ctx, msg.Actor.ID)
			if err != nil {
				log.Printf("inspect service %s failed: %v", msg.Actor.ID, err)
				return
			}
			w.cache.SetService(svc)
		}

	case events.ConfigEventType:
		if msg.Action == "remove" {
			w.cache.DeleteConfig(msg.Actor.ID)
		} else {
			cfg, err := w.client.InspectConfig(ctx, msg.Actor.ID)
			if err != nil {
				log.Printf("inspect config %s failed: %v", msg.Actor.ID, err)
				return
			}
			w.cache.SetConfig(cfg)
		}

	case events.SecretEventType:
		if msg.Action == "remove" {
			w.cache.DeleteSecret(msg.Actor.ID)
		} else {
			sec, err := w.client.InspectSecret(ctx, msg.Actor.ID)
			if err != nil {
				log.Printf("inspect secret %s failed: %v", msg.Actor.ID, err)
				return
			}
			w.cache.SetSecret(sec)
		}

	case events.NetworkEventType:
		if msg.Action == "remove" || msg.Action == "destroy" {
			w.cache.DeleteNetwork(msg.Actor.ID)
		} else {
			net, err := w.client.InspectNetwork(ctx, msg.Actor.ID)
			if err != nil {
				log.Printf("inspect network %s failed: %v", msg.Actor.ID, err)
				return
			}
			w.cache.SetNetwork(net)
		}

	case events.VolumeEventType:
		if msg.Action == "destroy" {
			w.cache.DeleteVolume(msg.Actor.ID)
		} else {
			vol, err := w.client.InspectVolume(ctx, msg.Actor.ID)
			if err != nil {
				log.Printf("inspect volume %s failed: %v", msg.Actor.ID, err)
				return
			}
			w.cache.SetVolume(vol)
		}

	case events.ContainerEventType:
		// Container events indicate task state changes.
		taskID := msg.Actor.Attributes["com.docker.swarm.task.id"]
		if taskID != "" {
			svcName := msg.Actor.Attributes["com.docker.swarm.service.name"]
			task, err := w.client.InspectTask(ctx, taskID)
			if err != nil {
				log.Printf("inspect task %s (svc: %s) failed: %v", taskID, svcName, err)
				return
			}
			w.cache.SetTask(task)
		}
	}
}
