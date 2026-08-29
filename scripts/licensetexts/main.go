// Command licensetexts harvests the license and notice text of every
// dependency in the committed CycloneDX SBOM into a content-addressed pool
// embedded alongside it. Run via scripts/build-sbom.sh, not directly.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/radiergummi/cetacean/internal/api/sbom"
)

func main() {
	sbomPath := flag.String("sbom", "internal/api/sbom/sbom.cdx.json", "path to the CycloneDX SBOM")
	outPath := flag.String(
		"out",
		"internal/api/sbom/licensetexts.json",
		"path to write the artifact",
	)
	nodeModules := flag.String(
		"node-modules",
		"frontend/node_modules",
		"path to the npm package store",
	)
	flag.Parse()

	if err := run(*sbomPath, *outPath, *nodeModules); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(sbomPath, outPath, nodeModules string) error {
	raw, err := os.ReadFile(sbomPath)
	if err != nil {
		return fmt.Errorf("read SBOM: %w", err)
	}

	doc, err := sbom.Project(raw)
	if err != nil {
		return fmt.Errorf("project SBOM: %w", err)
	}

	goModCache, err := goEnv("GOMODCACHE")
	if err != nil {
		return err
	}

	artifact, err := Harvest(doc, Roots{GoModCache: goModCache, NodeModules: nodeModules})
	if err != nil {
		return err
	}

	// MarshalIndent sorts map keys, so the committed file is stable across
	// runs; without that the sbom-check gate would fail at random.
	encoded, err := json.MarshalIndent(artifact, "", " ")
	if err != nil {
		return fmt.Errorf("encode artifact: %w", err)
	}

	//nolint:gosec // outPath is a -out build-tool flag, not user input
	if err := os.WriteFile(outPath, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}

	fmt.Printf(
		"wrote %s (%d components, %d distinct texts)\n",
		outPath, len(artifact.Components), len(artifact.Texts),
	)

	return nil
}

func goEnv(name string) (string, error) {
	//nolint:noctx // short-lived build-tool invocation, no cancellation to propagate
	out, err := exec.Command("go", "env", name).Output()
	if err != nil {
		return "", fmt.Errorf("go env %s: %w", name, err)
	}

	value := strings.TrimSpace(string(out))
	if value == "" {
		return "", fmt.Errorf("go env %s is empty", name)
	}

	return value, nil
}
