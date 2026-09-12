//go:build e2e

// Command e2eenv brings the end-to-end environment up and leaves it running,
// so the Playwright suite in frontend/e2e can be pointed at a reproducible
// cluster instead of whatever the developer's machine happens to hold.
//
// With -history it instead relabels the baseline; see
// fixtures.TouchForHistoryCLI.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
)

func main() {
	history := flag.Bool(
		"history",
		false,
		"relabel the baseline so the running SUT records change history for it",
	)
	flag.Parse()

	if err := run(*history); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run(history bool) error {
	env, err := harness.UpCLI()
	if err != nil {
		return fmt.Errorf("bring up environment: %w", err)
	}

	if history {
		if err := fixtures.TouchForHistoryCLI(env); err != nil {
			return fmt.Errorf("touch fixtures: %w", err)
		}

		return nil
	}

	if err := env.SwarmInitCLI(); err != nil {
		return fmt.Errorf("swarm init: %w", err)
	}

	if err := fixtures.DeployBaselineCLI(env); err != nil {
		return fmt.Errorf("deploy fixtures: %w", err)
	}

	if err := fixtures.DeployBrowserExtrasCLI(env); err != nil {
		return fmt.Errorf("deploy browser extras: %w", err)
	}

	// The browser suite skips every metrics spec when Prometheus reports
	// nothing, which is most of what the dashboard draws. Seeding here is what
	// lets those specs run, against the same numbers metrics_test.go asserts.
	address, hostname, err := fixtures.NodeIdentityCLI(env)
	if err != nil {
		return fmt.Errorf("resolve node: %w", err)
	}

	if _, err := harness.SeedPrometheusCLI(
		context.Background(),
		fixtures.MetricsSeed(address, hostname),
	); err != nil {
		return fmt.Errorf("seed prometheus: %w", err)
	}

	fmt.Println(env.DockerHost)

	return nil
}
