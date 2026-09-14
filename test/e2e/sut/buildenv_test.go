//go:build e2e

// White-box test for buildEnv: it needs access to the unexported function, so
// it lives in package sut rather than alongside the black-box tests in
// sut_test.go (package sut_test).
package sut

import (
	"slices"
	"strings"
	"testing"
)

func TestBuildEnvCarriesGOCOVERDIR(t *testing.T) {
	t.Setenv("GOCOVERDIR", "/tmp/cetacean-cov")

	env := buildEnv(Config{Port: 19001, DockerHost: "tcp://127.0.0.1:12375"})

	if !slices.Contains(env, "GOCOVERDIR=/tmp/cetacean-cov") {
		t.Errorf(
			"GOCOVERDIR not carried into the child environment; coverage from the "+
				"SUT would be silently lost.\nenv = %v",
			env,
		)
	}
}

func TestBuildEnvOmitsGOCOVERDIRWhenUnset(t *testing.T) {
	t.Setenv("GOCOVERDIR", "")

	env := buildEnv(Config{Port: 19001, DockerHost: "tcp://127.0.0.1:12375"})

	for _, kv := range env {
		if strings.HasPrefix(kv, "GOCOVERDIR=") {
			t.Errorf(
				"child environment names GOCOVERDIR when the parent does not: %q. "+
					"An uninstrumented binary would then try to write a profile.",
				kv,
			)
		}
	}
}
