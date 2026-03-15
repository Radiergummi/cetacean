package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFile_Empty(t *testing.T) {
	fc, err := LoadFile("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fc != nil {
		t.Error("expected nil config for empty path")
	}
}

func TestLoadFile_NotFound(t *testing.T) {
	_, err := LoadFile("/nonexistent/path.toml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadFile_InvalidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(path, []byte("not valid toml [[["), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFile(path)
	if err == nil {
		t.Error("expected error for invalid TOML")
	}
}

func TestLoadFile_FullConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	if err := os.WriteFile(path, []byte(`
[server]
listen_addr = ":7070"
pprof = true

[server.sse]
batch_interval = "50ms"

[docker]
host = "tcp://myhost:2375"

[prometheus]
url = "http://myprom:9090"

[logging]
level = "debug"
format = "text"

[storage]
data_dir = "/var/lib/cetacean"
snapshot = false
`), 0644); err != nil {
		t.Fatal(err)
	}

	fc, err := LoadFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fc.Server == nil || *fc.Server.ListenAddr != ":7070" {
		t.Error("ListenAddr not parsed")
	}
	if fc.Server.Pprof == nil || *fc.Server.Pprof != true {
		t.Error("Pprof not parsed")
	}
	if fc.Server.SSE == nil || *fc.Server.SSE.BatchInterval != "50ms" {
		t.Error("SSE BatchInterval not parsed")
	}
	if fc.Docker == nil || *fc.Docker.Host != "tcp://myhost:2375" {
		t.Error("Docker Host not parsed")
	}
	if fc.Prom == nil || *fc.Prom.URL != "http://myprom:9090" {
		t.Error("Prometheus URL not parsed")
	}
	if fc.Logging == nil || *fc.Logging.Level != "debug" {
		t.Error("Logging Level not parsed")
	}
	if fc.Logging.Format == nil || *fc.Logging.Format != "text" {
		t.Error("Logging Format not parsed")
	}
	if fc.Storage == nil || *fc.Storage.DataDir != "/var/lib/cetacean" {
		t.Error("Storage DataDir not parsed")
	}
	if fc.Storage.Snapshot == nil || *fc.Storage.Snapshot != false {
		t.Error("Storage Snapshot not parsed")
	}
}

func TestLoadFile_PartialConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "partial.toml")
	if err := os.WriteFile(path, []byte(`
[prometheus]
url = "http://prom:9090"
`), 0644); err != nil {
		t.Fatal(err)
	}

	fc, err := LoadFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fc.Server != nil {
		t.Error("Server should be nil for partial config")
	}
	if fc.Prom == nil || *fc.Prom.URL != "http://prom:9090" {
		t.Error("Prometheus URL not parsed")
	}
}
