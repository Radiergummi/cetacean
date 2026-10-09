package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	json "github.com/goccy/go-json"
)

const snapshotVersion = 2

type DiskSnapshot struct {
	Version   int                     `json:"version"`
	Timestamp time.Time               `json:"timestamp"`
	Nodes     []swarm.Node            `json:"nodes"`
	Services  []swarm.Service         `json:"services"`
	Tasks     []swarm.Task            `json:"tasks"`
	Configs   []swarm.Config          `json:"configs"`
	Secrets   []swarm.Secret          `json:"secrets"`
	Networks  []network.Summary       `json:"networks"`
	Volumes   []volume.Volume         `json:"volumes"`
	Restarts  *RestartTrackerSnapshot `json:"restarts,omitempty"`
}

// WriteToDisk serializes the cache to a JSON file using atomic rename.
func (c *Cache) WriteToDisk(path string) error {
	c.mu.RLock()
	snap := DiskSnapshot{
		Version:   snapshotVersion,
		Timestamp: time.Now(),
		Nodes:     c.nodes.list(),
		Services:  mapValues(c.services),
		Tasks:     mapValues(c.tasks),
		Configs:   c.configs.list(),
		Secrets:   c.secrets.list(),
		Networks:  c.networks.list(),
		Volumes:   c.volumes.list(),
	}
	c.mu.RUnlock()

	restarts := c.restarts.Snapshot()
	snap.Restarts = &restarts

	// Never persist secret data to disk.
	for i := range snap.Secrets {
		snap.Secrets[i].Spec.Data = nil
	}

	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := writeSynced(tmpPath, data); err != nil {
		os.Remove(tmpPath) //nolint:errcheck
		return fmt.Errorf("write snapshot tmp: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) //nolint:errcheck
		return fmt.Errorf("rename snapshot: %w", err)
	}

	// The rename only survives a power loss once the directory entry is on disk.
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("sync snapshot dir: %w", err)
	}
	defer dir.Close() //nolint:errcheck // read-only handle

	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync snapshot dir: %w", err)
	}

	return nil
}

// writeSynced writes data to path and flushes it to disk before returning, so
// the rename that publishes it never exposes a file the kernel has not written.
func writeSynced(path string, data []byte) error {
	f, err := os.OpenFile(
		path,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
		0o600,
	) //nolint:gosec // operator-configured data dir
	if err != nil {
		return err
	}

	if _, err := f.Write(data); err != nil {
		f.Close() //nolint:errcheck,gosec // the write error is the one to report
		return err
	}

	if err := f.Sync(); err != nil {
		f.Close() //nolint:errcheck,gosec // the sync error is the one to report
		return err
	}

	return f.Close()
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

	if snap.Version < 1 || snap.Version > snapshotVersion {
		return fmt.Errorf(
			"unsupported snapshot version: got %d, supported 1..%d",
			snap.Version,
			snapshotVersion,
		)
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

	if snap.Restarts != nil {
		c.restarts.Restore(*snap.Restarts)
	}

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
		return time.Duration(1<<63 - 1) // max duration — never synced
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
