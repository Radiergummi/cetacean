//go:build e2e

// Command e2eenv brings the end-to-end environment up and leaves it running,
// so the Playwright suite in frontend/e2e can be pointed at a reproducible
// cluster instead of whatever the developer's machine happens to hold.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
)

func main() {
	env, err := harness.UpCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bring up environment: %v\n", err)
		os.Exit(1)
	}

	if err := env.SwarmInitCLI(); err != nil {
		fmt.Fprintf(os.Stderr, "swarm init: %v\n", err)
		os.Exit(1)
	}

	if err := fixtures.DeployBaselineCLI(env); err != nil {
		fmt.Fprintf(os.Stderr, "deploy fixtures: %v\n", err)
		os.Exit(1)
	}

	// The browser suite skips every metrics spec when Prometheus reports
	// nothing, which is most of what the dashboard draws. Seeding here is what
	// lets those specs run, against the same numbers metrics_test.go asserts.
	address, hostname, err := fixtures.NodeIdentityCLI(env)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve node: %v\n", err)
		os.Exit(1)
	}

	if _, err := harness.SeedPrometheusCLI(
		context.Background(),
		fixtures.MetricsSeed(address, hostname),
	); err != nil {
		fmt.Fprintf(os.Stderr, "seed prometheus: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(env.DockerHost)
}
