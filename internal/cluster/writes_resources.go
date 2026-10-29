package cluster

import (
	"context"
	"log/slog"

	"github.com/docker/docker/api/types/swarm"
)

// UpdateNodeAvailability sets whether a node takes new tasks or keeps its own.
func (w *Writes) UpdateNodeAvailability(
	ctx context.Context,
	c Caller,
	key string,
	availability swarm.NodeAvailability,
) (swarm.Node, error) {
	node, err := w.record(c, OpUpdateNode, "node", key)
	if err != nil {
		return swarm.Node{}, err
	}

	slog.InfoContext(ctx, "updating node availability",
		"node", node.ID, "availability", availability)
	updated, err := w.docker.UpdateNodeAvailability(ctx, node.ID, availability)

	return updated, classify(err, "node", key)
}

// UpdateNodeRole promotes a worker or demotes a manager.
func (w *Writes) UpdateNodeRole(
	ctx context.Context,
	c Caller,
	key string,
	role swarm.NodeRole,
) (swarm.Node, error) {
	node, err := w.record(c, OpUpdateNode, "node", key)
	if err != nil {
		return swarm.Node{}, err
	}

	slog.InfoContext(ctx, "updating node role", "node", node.ID, "role", role)
	updated, err := w.docker.UpdateNodeRole(ctx, node.ID, role)

	return updated, classify(err, "node", key)
}

// UpdateNodeLabels applies change to a node's labels as Docker holds them.
func (w *Writes) UpdateNodeLabels(
	ctx context.Context,
	c Caller,
	key string,
	change MapChange,
) (swarm.Node, error) {
	node, err := w.record(c, OpUpdateNodeLabels, "node", key)
	if err != nil {
		return swarm.Node{}, err
	}

	slog.InfoContext(ctx, "updating node labels", "node", node.ID)
	updated, err := w.docker.UpdateNodeLabels(ctx, node.ID, change)

	return updated, classify(err, "node", key)
}

// RemoveService removes a service and its tasks.
func (w *Writes) RemoveService(ctx context.Context, c Caller, key string) error {
	return w.remove(ctx, c, OpRemoveService, "service", key, w.docker.RemoveService)
}

// RemoveTask removes a task, which its service replaces.
func (w *Writes) RemoveTask(ctx context.Context, c Caller, key string) error {
	return w.remove(ctx, c, OpRemoveTask, "task", key, w.docker.RemoveTask)
}

// RemoveConfig removes a config no service uses.
func (w *Writes) RemoveConfig(ctx context.Context, c Caller, key string) error {
	return w.remove(ctx, c, OpRemoveConfig, "config", key, w.docker.RemoveConfig)
}

// RemoveSecret removes a secret no service uses.
func (w *Writes) RemoveSecret(ctx context.Context, c Caller, key string) error {
	return w.remove(ctx, c, OpRemoveSecret, "secret", key, w.docker.RemoveSecret)
}

// RemoveNetwork removes a network no service is attached to.
func (w *Writes) RemoveNetwork(ctx context.Context, c Caller, key string) error {
	return w.remove(ctx, c, OpRemoveNetwork, "network", key, w.docker.RemoveNetwork)
}

// RemoveVolume removes a volume. On a local volume force overrides the
// driver's errors, not use; Docker refuses an in-use local volume either way.
func (w *Writes) RemoveVolume(ctx context.Context, c Caller, name string, force bool) error {
	return w.remove(
		ctx,
		c,
		OpRemoveVolume,
		"volume",
		name,
		func(ctx context.Context, id string) error {
			return w.docker.RemoveVolume(ctx, id, force)
		},
	)
}

func (w *Writes) remove(
	ctx context.Context,
	c Caller,
	op Operation,
	kind, key string,
	remove func(ctx context.Context, id string) error,
) error {
	record, err := w.record(c, op, kind, key)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "removing "+kind, kind, record.ID)
	if err := remove(ctx, record.ID); err != nil {
		if refusal, ok := ClassifyRemoval(err, kind, key); ok {
			return refusal
		}

		return err
	}

	return nil
}

// CreateConfig creates a config and seeds it into the cache, so the next
// write that names it resolves before the watcher has seen it.
func (w *Writes) CreateConfig(
	ctx context.Context,
	c Caller,
	name string,
	labels map[string]string,
	data []byte,
) (string, error) {
	if err := w.creatable(c, OpCreateConfig, "config", name, labels); err != nil {
		return "", err
	}

	slog.InfoContext(ctx, "creating config", "name", name)
	id, err := w.docker.CreateConfig(ctx, swarm.ConfigSpec{Name: name, Labels: labels, Data: data})
	if err != nil {
		return "", classify(err, "config", name)
	}

	SeedCreatedConfig(ctx, w.cache, w.docker.InspectConfig, id, name, labels)

	return id, nil
}

// CreateSecret is CreateConfig for a secret.
func (w *Writes) CreateSecret(
	ctx context.Context,
	c Caller,
	name string,
	labels map[string]string,
	data []byte,
) (string, error) {
	if err := w.creatable(c, OpCreateSecret, "secret", name, labels); err != nil {
		return "", err
	}

	slog.InfoContext(ctx, "creating secret", "name", name)
	id, err := w.docker.CreateSecret(ctx, swarm.SecretSpec{Name: name, Labels: labels, Data: data})
	if err != nil {
		return "", classify(err, "secret", name)
	}

	SeedCreatedSecret(ctx, w.cache, w.docker.InspectSecret, id, name, labels)

	return id, nil
}

// creatable judges a create, which has no record yet: the grant is on the
// name it will have, and its labels may not put it into a stack the caller
// cannot write.
func (w *Writes) creatable(
	c Caller,
	op Operation,
	kind, name string,
	labels map[string]string,
) error {
	resource := kind + ":" + name
	if !c.CanWrite(resource) {
		return denied(kind, resource)
	}
	if err := belowTier(c, op, kind); err != nil {
		return err
	}

	return classify(CheckStackLabel(nil, labels, c.CanWrite), kind, name)
}
