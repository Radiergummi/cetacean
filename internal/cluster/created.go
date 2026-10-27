package cluster

import (
	"context"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// SeedCreatedConfig puts a config just created into the cache, which the
// watcher fills only later, so the next call can attach it. It is Docker's own
// record, so the creation time is the manager's and the newest version is the
// newest at once; a stand-in names it when Docker cannot be asked. The payload
// is left to the watcher.
func SeedCreatedConfig(
	ctx context.Context,
	c *cache.Cache,
	inspect func(ctx context.Context, id string) (swarm.Config, error),
	id, name string,
	labels map[string]string,
) {
	cfg, err := inspect(ctx, id)
	if err != nil {
		cfg = swarm.Config{ID: id, Spec: swarm.ConfigSpec{Name: name, Labels: labels}}
	}

	cfg.Spec.Data = nil
	c.SetConfig(cfg)
}

// SeedCreatedSecret is SeedCreatedConfig for a secret, whose value is never
// cached.
func SeedCreatedSecret(
	ctx context.Context,
	c *cache.Cache,
	inspect func(ctx context.Context, id string) (swarm.Secret, error),
	id, name string,
	labels map[string]string,
) {
	sec, err := inspect(ctx, id)
	if err != nil {
		sec = swarm.Secret{ID: id, Spec: swarm.SecretSpec{Name: name, Labels: labels}}
	}

	c.SetSecret(RedactSecret(sec))
}
