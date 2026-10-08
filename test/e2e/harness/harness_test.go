//go:build e2e

package harness_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/radiergummi/cetacean/test/e2e/harness"
)

func TestUpBringsUpASwarmEngine(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	info, err := env.Docker.Info(t.Context())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}

	if !info.Swarm.ControlAvailable {
		t.Errorf("engine is not a swarm manager: %+v", info.Swarm)
	}
}

func TestUpExposesTheCertificateChain(t *testing.T) {
	env := harness.Up(t)

	for _, name := range []string{"ca.pem", "server.pem", "server-key.pem", "client.pem", "client-key.pem"} {
		if _, err := os.Stat(filepath.Join(env.CertDir, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
}
