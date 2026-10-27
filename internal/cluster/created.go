package cluster

import (
	"context"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// SeedCreatedConfig puts a just-created config into the cache, so the next call
// can attach it before the watcher catches up: Docker's own record, or else a
// stand-in created now. A record the watcher already stored is never replaced.
func SeedCreatedConfig(
	ctx context.Context,
	c *cache.Cache,
	inspect func(ctx context.Context, id string) (swarm.Config, error),
	id, name string,
	labels map[string]string,
) {
	cfg, err := inspect(ctx, id)
	if err != nil {
		slog.Warn(
			"inspecting a created config failed; seeding a stand-in",
			"config",
			name,
			"error",
			err,
		)
		now := time.Now()
		cfg = swarm.Config{
			ID:        id,
			CreatedAt: now, UpdatedAt: now,
			Spec: swarm.ConfigSpec{Name: name, Labels: labels},
		}
	}

	c.AddConfig(cfg)
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
		slog.Warn(
			"inspecting a created secret failed; seeding a stand-in",
			"secret",
			name,
			"error",
			err,
		)
		now := time.Now()
		sec = swarm.Secret{
			ID:        id,
			CreatedAt: now, UpdatedAt: now,
			Spec: swarm.SecretSpec{Name: name, Labels: labels},
		}
	}

	c.AddSecret(RedactSecret(sec))
}
