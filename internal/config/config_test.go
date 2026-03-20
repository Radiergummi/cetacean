package config

import (
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("CETACEAN_DOCKER_HOST", "")
	t.Setenv("CETACEAN_PROMETHEUS_URL", "")
	t.Setenv("CETACEAN_LISTEN_ADDR", "")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PrometheusURL != "" {
		t.Errorf("expected empty PrometheusURL, got %s", cfg.PrometheusURL)
	}
}

func TestLoad_WithRequiredEnv(t *testing.T) {
	t.Setenv("CETACEAN_PROMETHEUS_URL", "http://prometheus:9090")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DockerHost != "unix:///var/run/docker.sock" {
		t.Errorf("expected default docker host, got %s", cfg.DockerHost)
	}
	if cfg.PrometheusURL != "http://prometheus:9090" {
		t.Errorf("expected prometheus URL, got %s", cfg.PrometheusURL)
	}
	if cfg.ListenAddr != ":9000" {
		t.Errorf("expected default listen addr, got %s", cfg.ListenAddr)
	}
}

func TestLoad_AllEnvVars(t *testing.T) {
	t.Setenv("CETACEAN_DOCKER_HOST", "tcp://remote:2375")
	t.Setenv("CETACEAN_PROMETHEUS_URL", "http://prom:9090")
	t.Setenv("CETACEAN_LISTEN_ADDR", ":8080")
	t.Setenv("CETACEAN_LOG_LEVEL", "debug")
	t.Setenv("CETACEAN_LOG_FORMAT", "text")
	t.Setenv("CETACEAN_DATA_DIR", "/tmp/data")
	t.Setenv("CETACEAN_SNAPSHOT", "false")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DockerHost != "tcp://remote:2375" {
		t.Errorf("got %s", cfg.DockerHost)
	}
	if cfg.PrometheusURL != "http://prom:9090" {
		t.Errorf("got %s", cfg.PrometheusURL)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("got %s", cfg.ListenAddr)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel=%s, want debug", cfg.LogLevel)
	}
	if cfg.LogFormat != "text" {
		t.Errorf("LogFormat=%s, want text", cfg.LogFormat)
	}
	if cfg.DataDir != "/tmp/data" {
		t.Errorf("DataDir=%s, want /tmp/data", cfg.DataDir)
	}
	if cfg.Snapshot != false {
		t.Errorf("Snapshot=%v, want false", cfg.Snapshot)
	}
	if cfg.SSEBatchInterval != 100*time.Millisecond {
		t.Errorf("SSEBatchInterval=%v, want 100ms", cfg.SSEBatchInterval)
	}
}

func TestSlogLevel(t *testing.T) {
	tests := []struct {
		level string
		want  string
	}{
		{"debug", "DEBUG"},
		{"warn", "WARN"},
		{"error", "ERROR"},
		{"info", "INFO"},
		{"", "INFO"},        // default
		{"WARN", "WARN"},    // case insensitive
		{"unknown", "INFO"}, // fallback
	}
	for _, tt := range tests {
		cfg := &Config{LogLevel: tt.level}
		got := cfg.SlogLevel().String()
		if got != tt.want {
			t.Errorf("SlogLevel(%q)=%s, want %s", tt.level, got, tt.want)
		}
	}
}

func TestLoad_SSEBatchInterval(t *testing.T) {
	t.Setenv("CETACEAN_PROMETHEUS_URL", "http://prom:9090")
	t.Setenv("CETACEAN_SSE_BATCH_INTERVAL", "200ms")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SSEBatchInterval != 200*time.Millisecond {
		t.Errorf("SSEBatchInterval=%v, want 200ms", cfg.SSEBatchInterval)
	}
}

func TestLoad_FlagOverridesEnv(t *testing.T) {
	t.Setenv("CETACEAN_LISTEN_ADDR", ":8080")

	listen := ":9090"
	flags := &Flags{Listen: &listen}

	cfg, err := Load(nil, flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr=%s, want :9090 (flag should override env)", cfg.ListenAddr)
	}
}

func TestLoad_EnvOverridesFile(t *testing.T) {
	t.Setenv("CETACEAN_LISTEN_ADDR", ":8080")

	listen := ":7070"
	fc := &fileConfig{
		Server: &fileServer{ListenAddr: &listen},
	}

	cfg, err := Load(fc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr=%s, want :8080 (env should override file)", cfg.ListenAddr)
	}
}

func TestLoad_FileOverridesDefault(t *testing.T) {
	t.Setenv("CETACEAN_LISTEN_ADDR", "")

	listen := ":7070"
	fc := &fileConfig{
		Server: &fileServer{ListenAddr: &listen},
	}

	cfg, err := Load(fc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != ":7070" {
		t.Errorf("ListenAddr=%s, want :7070 (file should override default)", cfg.ListenAddr)
	}
}

func TestLoad_FullPrecedence(t *testing.T) {
	t.Setenv("CETACEAN_LISTEN_ADDR", ":8080")

	fileListen := ":7070"
	fc := &fileConfig{
		Server: &fileServer{ListenAddr: &fileListen},
	}

	flagListen := ":9090"
	flags := &Flags{Listen: &flagListen}

	cfg, err := Load(fc, flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr=%s, want :9090 (flag > env > file > default)", cfg.ListenAddr)
	}
}

func TestLoad_OperationsLevel_Default(t *testing.T) {
	t.Setenv("CETACEAN_OPERATIONS_LEVEL", "")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OperationsLevel != OpsOperational {
		t.Errorf("OperationsLevel=%d, want %d", cfg.OperationsLevel, OpsOperational)
	}
}

func TestLoad_OperationsLevel_EnvOverride(t *testing.T) {
	t.Setenv("CETACEAN_OPERATIONS_LEVEL", "0")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OperationsLevel != OpsReadOnly {
		t.Errorf("OperationsLevel=%d, want %d", cfg.OperationsLevel, OpsReadOnly)
	}
}

func TestLoad_OperationsLevel_FileOverride(t *testing.T) {
	t.Setenv("CETACEAN_OPERATIONS_LEVEL", "")

	level := int(OpsImpactful)
	fc := &fileConfig{
		Server: &fileServer{OperationsLevel: &level},
	}

	cfg, err := Load(fc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OperationsLevel != OpsImpactful {
		t.Errorf("OperationsLevel=%d, want %d", cfg.OperationsLevel, OpsImpactful)
	}
}

func TestLoad_OperationsLevel_OutOfRange(t *testing.T) {
	t.Setenv("CETACEAN_OPERATIONS_LEVEL", "5")

	_, err := Load(nil, nil)
	if err == nil {
		t.Fatal("expected error for out-of-range value")
	}
}

func TestLoad_OperationsLevel_Negative(t *testing.T) {
	t.Setenv("CETACEAN_OPERATIONS_LEVEL", "-1")

	_, err := Load(nil, nil)
	if err == nil {
		t.Fatal("expected error for negative value")
	}
}

func TestLoad_OperationsLevel_Invalid(t *testing.T) {
	t.Setenv("CETACEAN_OPERATIONS_LEVEL", "banana")

	_, err := Load(nil, nil)
	if err == nil {
		t.Fatal("expected error for non-integer value")
	}
}

func TestLoad_FileConfig(t *testing.T) {
	t.Setenv("CETACEAN_DOCKER_HOST", "")
	t.Setenv("CETACEAN_PROMETHEUS_URL", "")
	t.Setenv("CETACEAN_LISTEN_ADDR", "")
	t.Setenv("CETACEAN_LOG_LEVEL", "")
	t.Setenv("CETACEAN_LOG_FORMAT", "")
	t.Setenv("CETACEAN_DATA_DIR", "")
	t.Setenv("CETACEAN_SNAPSHOT", "")
	t.Setenv("CETACEAN_PPROF", "")

	listen := ":7070"
	pprof := true
	batch := "50ms"
	host := "tcp://myhost:2375"
	promURL := "http://myprom:9090"
	level := "debug"
	format := "text"
	dataDir := "/var/lib/cetacean"
	snapshot := false

	fc := &fileConfig{
		Server: &fileServer{
			ListenAddr: &listen,
			Pprof:      &pprof,
			SSE:        &fileSSE{BatchInterval: &batch},
		},
		Docker:  &fileDocker{Host: &host},
		Prom:    &fileProm{URL: &promURL},
		Logging: &fileLogging{Level: &level, Format: &format},
		Storage: &fileStorage{DataDir: &dataDir, Snapshot: &snapshot},
	}

	cfg, err := Load(fc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != ":7070" {
		t.Errorf("ListenAddr=%s", cfg.ListenAddr)
	}
	if cfg.Pprof != true {
		t.Errorf("Pprof=%v", cfg.Pprof)
	}
	if cfg.SSEBatchInterval != 50*time.Millisecond {
		t.Errorf("SSEBatchInterval=%v", cfg.SSEBatchInterval)
	}
	if cfg.DockerHost != "tcp://myhost:2375" {
		t.Errorf("DockerHost=%s", cfg.DockerHost)
	}
	if cfg.PrometheusURL != "http://myprom:9090" {
		t.Errorf("PrometheusURL=%s", cfg.PrometheusURL)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel=%s", cfg.LogLevel)
	}
	if cfg.LogFormat != "text" {
		t.Errorf("LogFormat=%s", cfg.LogFormat)
	}
	if cfg.DataDir != "/var/lib/cetacean" {
		t.Errorf("DataDir=%s", cfg.DataDir)
	}
	if cfg.Snapshot != false {
		t.Errorf("Snapshot=%v", cfg.Snapshot)
	}
}
