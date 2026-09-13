//go:build e2e

package sut_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

func TestStartServesReady(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := sut.Start(t, sut.Config{
		Port:       19001,
		DockerHost: env.DockerHost,
		Env:        map[string]string{"CETACEAN_AUTH_MODE": "none"},
	})

	resp, err := proc.Client().Get(proc.BaseURL + "/-/health")
	if err != nil {
		t.Fatalf("GET /-/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// The environment handed to the child must be exactly what the case names.
// A CETACEAN_* variable in the developer's shell silently changing a result
// is the failure mode that makes a suite untrustworthy.
func TestStartDoesNotInheritTheParentEnvironment(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "headers")

	env := harness.Up(t)
	env.SwarmInit(t)

	proc := sut.Start(t, sut.Config{
		Port:       19001,
		DockerHost: env.DockerHost,
		Env:        map[string]string{"CETACEAN_AUTH_MODE": "none"},
	})

	resp, err := proc.Client().Get(proc.BaseURL + "/auth/whoami")
	if err != nil {
		t.Fatalf("GET /auth/whoami: %v", err)
	}
	defer resp.Body.Close()

	// headers mode without a configured subject header would have refused to
	// start; reaching whoami at all proves "none" won.
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestStartExpectingExitReportsAStartupRefusal(t *testing.T) {
	env := harness.Up(t)

	code, output := sut.StartExpectingExit(t, sut.Config{
		Port:       19004,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE": "headers",
			// headers mode requires a subject header; omitting it must refuse.
		},
	})

	if code == 0 {
		t.Errorf("exit code = 0, want non-zero; output:\n%s", output)
	}

	if !strings.Contains(output, "headers") {
		t.Errorf("output does not mention the offending setting:\n%s", output)
	}
}
