//go:build e2e

// Package harness owns the lifecycle of the end-to-end environment: the
// compose project holding the Docker-in-Docker engine and its supporting
// services, and a client connected to that engine.
package harness

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

const (
	dockerHost   = "tcp://127.0.0.1:12375"
	upTimeout    = 90 * time.Second
	pollInterval = 500 * time.Millisecond
)

// Env is a running end-to-end environment.
type Env struct {
	DockerHost string
	CertDir    string
	Docker     *client.Client
}

var (
	once   sync.Once
	shared *Env
	upErr  error
)

// Up brings the environment up and returns it. The environment is shared by
// every test in a run — bringing up a fresh engine per test would cost more
// than the isolation is worth, and fixtures are namespaced instead.
//
// Up does NOT register a t.Cleanup teardown, and must not: the environment is
// shared process-wide through sync.Once, so a Cleanup scoped to whichever
// test happened to call Up first would tear it down under its sibling tests.
// Teardown is owned by the `make test-stack` recipe instead, which brings the
// environment down on a successful run and deliberately leaves it running on
// failure, so a failed case stays available for inspection.
func Up(t *testing.T) *Env {
	t.Helper()

	once.Do(func() { shared, upErr = up() })

	if upErr != nil {
		t.Fatalf("bring up environment: %v", upErr)
	}

	return shared
}

func up() (*Env, error) {
	root, err := repoRoot()
	if err != nil {
		return nil, err
	}

	composeFile := filepath.Join(root, "test", "e2e", "compose.e2e.yaml")

	// No --wait: cert-init is a one-shot that exits, which --wait treats as a
	// failure. The explicit polls below are the stronger check anyway.
	cmd := exec.Command("docker", "compose", "-f", composeFile, "up", "-d")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker compose up: %w", err)
	}

	certDir := filepath.Join(root, "test", "e2e", "certs")
	if err := waitForCerts(certDir); err != nil {
		return nil, err
	}

	docker, err := client.NewClientWithOpts(
		client.WithHost(dockerHost),
		client.WithVersion("1.46"),
	)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}

	if err := waitForEngine(docker); err != nil {
		return nil, err
	}

	return &Env{DockerHost: dockerHost, CertDir: certDir, Docker: docker}, nil
}

// waitForCerts blocks until cert-init has written the whole chain. The
// container exits when done, so `--wait` does not cover it.
func waitForCerts(dir string) error {
	deadline := time.Now().Add(upTimeout)
	want := []string{"ca.pem", "server.pem", "server-key.pem", "client.pem", "client-key.pem"}

	for time.Now().Before(deadline) {
		missing := false

		for _, name := range want {
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				missing = true

				break
			}
		}

		if !missing {
			return nil
		}

		time.Sleep(pollInterval)
	}

	return fmt.Errorf("certificates not written to %s within %s", dir, upTimeout)
}

func waitForEngine(docker *client.Client) error {
	deadline := time.Now().Add(upTimeout)

	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := docker.Ping(ctx)
		cancel()

		if err == nil {
			return nil
		}

		time.Sleep(pollInterval)
	}

	return fmt.Errorf("engine at %s not reachable within %s", dockerHost, upTimeout)
}

// SwarmInit makes the engine a swarm manager. It is idempotent: an engine
// that is already a manager is left alone.
func (e *Env) SwarmInit(t *testing.T) {
	t.Helper()

	info, err := e.Docker.Info(t.Context())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}

	if info.Swarm.ControlAvailable {
		return
	}

	if _, err := e.Docker.SwarmInit(t.Context(), swarm.InitRequest{
		ListenAddr:    "0.0.0.0:2377",
		AdvertiseAddr: "127.0.0.1",
	}); err != nil {
		t.Fatalf("SwarmInit: %v", err)
	}
}

// repoRoot walks up from this source file to the module root, so the harness
// works regardless of which package's tests invoked it.
func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot locate harness source")
	}

	// .../test/e2e/harness/harness.go -> repo root is four levels up.
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..")), nil
}
