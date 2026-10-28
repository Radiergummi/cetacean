package cluster

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// The watcher can store the created config before the create returns; the
// seed must leave that record, payload and all, as it is.
func TestSeedCreatedConfigKeepsWhatTheWatcherStored(t *testing.T) {
	c := cache.New(nil)
	stored := swarm.Config{
		ID:      "cfg",
		Version: swarm.Version{Index: 7},
		Spec:    swarm.ConfigSpec{Name: "app_v2", Data: []byte("content")},
	}
	c.SetConfig(stored)

	inspect := func(context.Context, string) (swarm.Config, error) {
		seeded := stored
		seeded.Spec.Data = nil

		return seeded, nil
	}
	SeedCreatedConfig(context.Background(), c, inspect, "cfg", "app_v2", nil)

	if got, _ := c.GetConfig("cfg"); string(got.Spec.Data) != "content" {
		t.Errorf("cached data = %q, want the watcher's record kept", got.Spec.Data)
	}
}

// Without Docker's record the stand-in is created now, so it ranks as the
// newest version rather than the oldest.
func TestSeedCreatedSecretStandInIsNew(t *testing.T) {
	c := cache.New(nil)
	before := time.Now()

	failing := func(context.Context, string) (swarm.Secret, error) {
		return swarm.Secret{}, errors.New("unreachable")
	}
	SeedCreatedSecret(context.Background(), c, failing, "sec", "db_v2", nil)

	got, ok := c.GetSecret("sec")
	if !ok || got.CreatedAt.Before(before) || got.Spec.Name != "db_v2" {
		t.Errorf("seeded = %+v, want a stand-in named db_v2 created now", got)
	}
}
