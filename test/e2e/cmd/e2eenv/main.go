//go:build e2e

// Command e2eenv brings the end-to-end environment up and leaves it running,
// so the Playwright suite in frontend/e2e can be pointed at a reproducible
// cluster instead of whatever the developer's machine happens to hold.
package main

import (
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

	fmt.Println(env.DockerHost)
}
