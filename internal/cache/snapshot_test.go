package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

func TestWriteAndLoadSnapshot(t *testing.T) {
	c := New(nil)
	c.SetNode(swarm.Node{ID: "n1", Description: swarm.NodeDescription{Hostname: "worker-01"}})
	c.SetService(swarm.Service{ID: "s1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "nginx"}}})

	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")

	err := c.WriteToDisk(path)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("snapshot file doesn't exist: %v", err)
	}

	c2 := New(nil)
	err = c2.LoadFromDisk(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	nodes := c2.ListNodes()
	if len(nodes) != 1 || nodes[0].Description.Hostname != "worker-01" {
		t.Errorf("expected 1 node with hostname worker-01, got %v", nodes)
	}

	services := c2.ListServices()
	if len(services) != 1 || services[0].Spec.Name != "nginx" {
		t.Errorf("expected 1 service named nginx, got %v", services)
	}
}

func TestLoadSnapshot_FileNotExists(t *testing.T) {
	c := New(nil)
	err := c.LoadFromDisk("/nonexistent/path")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadSnapshot_InvalidVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	if err := os.WriteFile(path, []byte(`{"version":999}`), 0644); err != nil {
		t.Fatal(err)
	}

	c := New(nil)
	err := c.LoadFromDisk(path)
	if err == nil {
		t.Error("expected error for invalid version")
	}
}

func TestWriteSnapshot_AtomicRename(t *testing.T) {
	c := New(nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")

	if err := c.WriteToDisk(path); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file should not exist after successful write")
	}
}

func TestSnapshotPreservesAllResourceTypes(t *testing.T) {
	c := New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	c.SetService(swarm.Service{ID: "s1"})
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "s1", NodeID: "n1"})
	c.SetConfig(swarm.Config{ID: "c1"})
	c.SetSecret(swarm.Secret{ID: "sec1"})
	c.SetNetwork(network.Summary{ID: "net1"})
	c.SetVolume(volume.Volume{Name: "vol1"})

	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")

	if err := c.WriteToDisk(path); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	c2 := New(nil)
	if err := c2.LoadFromDisk(path); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	snap := c2.Snapshot()
	if snap.NodeCount != 1 {
		t.Errorf("expected 1 node, got %d", snap.NodeCount)
	}
	if snap.ServiceCount != 1 {
		t.Errorf("expected 1 service, got %d", snap.ServiceCount)
	}
	if snap.TaskCount != 1 {
		t.Errorf("expected 1 task, got %d", snap.TaskCount)
	}

	if _, ok := c2.GetConfig("c1"); !ok {
		t.Error("expected config c1")
	}
	if _, ok := c2.GetSecret("sec1"); !ok {
		t.Error("expected secret sec1")
	}
	if _, ok := c2.GetNetwork("net1"); !ok {
		t.Error("expected network net1")
	}
	if _, ok := c2.GetVolume("vol1"); !ok {
		t.Error("expected volume vol1")
	}
}

func TestSnapshotAge(t *testing.T) {
	c := New(nil)

	// Before any sync, lastSync is zero — SnapshotAge should be large
	if c.SnapshotAge() < time.Hour {
		t.Error("expected large SnapshotAge before any sync")
	}

	// After ReplaceAll, SnapshotAge should be small
	c.ReplaceAll(FullSyncData{HasNodes: true, Nodes: []swarm.Node{{ID: "n1"}}})
	if c.SnapshotAge() > time.Second {
		t.Errorf("expected small SnapshotAge after ReplaceAll, got %v", c.SnapshotAge())
	}
}

func TestClusterSnapshot_LastSync(t *testing.T) {
	c := New(nil)
	c.ReplaceAll(FullSyncData{HasNodes: true})

	snap := c.Snapshot()
	if snap.LastSync.IsZero() {
		t.Error("expected non-zero LastSync after ReplaceAll")
	}
	if time.Since(snap.LastSync) > time.Second {
		t.Error("expected LastSync to be recent")
	}
}
