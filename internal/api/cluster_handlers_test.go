package api

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types"
)

// A caller leaving mid-fetch must not cost the next caller a fresh daemon walk.
func TestDiskUsageFetchOutlivesTheCaller(t *testing.T) {
	var c diskUsageCache
	calls := 0
	fetch := func(ctx context.Context) (types.DiskUsage, error) {
		calls++
		return types.DiskUsage{}, ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for range 5 {
		if _, err := c.get(ctx, fetch); err != nil {
			t.Fatalf("get with a cancelled caller: %v", err)
		}
	}

	if calls != 1 {
		t.Errorf("daemon called %d times, want 1", calls)
	}
}

func TestDiskUsageFailureIsSharedBriefly(t *testing.T) {
	var c diskUsageCache
	calls := 0
	fetch := func(context.Context) (types.DiskUsage, error) {
		calls++
		return types.DiskUsage{}, errors.New("daemon unavailable")
	}

	for range 3 {
		if _, err := c.get(context.Background(), fetch); err == nil {
			t.Fatal("get returned no error for a failed fetch")
		}
	}

	if calls != 1 {
		t.Errorf("daemon called %d times, want 1", calls)
	}
}
