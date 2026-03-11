package cache

import (
	"fmt"
	"os"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	json "github.com/goccy/go-json"
)

const snapshotVersion = 1

type DiskSnapshot struct {
	Version   int               `json:"version"`
	Timestamp time.Time         `json:"timestamp"`
	Nodes     []swarm.Node      `json:"nodes"`
	Services  []swarm.Service   `json:"services"`
	Tasks     []swarm.Task      `json:"tasks"`
	Configs   []swarm.Config    `json:"configs"`
	Secrets   []swarm.Secret    `json:"secrets"`
	Networks  []network.Summary `json:"networks"`
	Volumes   []volume.Volume   `json:"volumes"`
}

// WriteToDisk serializes the cache to a JSON file using atomic rename.
func (c *Cache) WriteToDisk(path string) error {
	c.mu.RLock()
	snap := DiskSnapshot{
		Version:   snapshotVersion,
		Timestamp: time.Now(),
		Nodes:     mapValues(c.nodes),
		Services:  mapValues(c.services),
		Tasks:     mapValues(c.tasks),
		Configs:   mapValues(c.configs),
		Secrets:   mapValues(c.secrets),
		Networks:  mapValues(c.networks),
		Volumes:   mapValues(c.volumes),
	}
	c.mu.RUnlock()

	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write snapshot tmp: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) //nolint:errcheck
		return fmt.Errorf("rename snapshot: %w", err)
	}

	return nil
}

// LoadFromDisk reads a snapshot file and populates the cache via ReplaceAll.
func (c *Cache) LoadFromDisk(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read snapshot: %w", err)
	}

	var snap DiskSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("unmarshal snapshot: %w", err)
	}

	if snap.Version != snapshotVersion {
		return fmt.Errorf("snapshot version mismatch: got %d, want %d", snap.Version, snapshotVersion)
	}

	c.ReplaceAll(FullSyncData{
		Nodes:       snap.Nodes,
		Services:    snap.Services,
		Tasks:       snap.Tasks,
		Configs:     snap.Configs,
		Secrets:     snap.Secrets,
		Networks:    snap.Networks,
		Volumes:     snap.Volumes,
		HasNodes:    true,
		HasServices: true,
		HasTasks:    true,
		HasConfigs:  true,
		HasSecrets:  true,
		HasNetworks: true,
		HasVolumes:  true,
	})

	// Set lastSync to the snapshot timestamp so SnapshotAge reflects staleness.
	c.mu.Lock()
	c.lastSync = snap.Timestamp
	c.mu.Unlock()

	return nil
}

// SnapshotAge returns the time since the cache was last populated.
func (c *Cache) SnapshotAge() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.lastSync.IsZero() {
		return time.Since(time.Time{})
	}
	return time.Since(c.lastSync)
}

func mapValues[K comparable, V any](m map[K]V) []V {
	out := make([]V, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
