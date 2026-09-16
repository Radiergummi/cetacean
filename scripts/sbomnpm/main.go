// Command sbomnpm renders the npm half of the CycloneDX SBOM from the pnpm
// lockfile. cyclonedx-npm cannot read a pnpm workspace and no other generator
// tested preserves the tarball hashes, which only the lockfile records.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"

	cyclonedx "github.com/CycloneDX/cyclonedx-go"
)

func main() {
	lockPath := flag.String("lockfile", "pnpm-lock.yaml", "path to pnpm-lock.yaml")
	importerName := flag.String(
		"importer",
		"frontend",
		"workspace package whose production closure to render",
	)
	store := flag.String(
		"store",
		"node_modules/.pnpm",
		"pnpm virtual store holding the unpacked packages",
	)
	ref := flag.String("bom-ref-prefix", "frontend@0.0.0", "prefix for each component's bom-ref")
	out := flag.String("out", "", "output file (default stdout)")
	flag.Parse()

	if err := run(*lockPath, *importerName, *store, *ref, *out); err != nil {
		fmt.Fprintln(os.Stderr, "sbomnpm:", err)
		os.Exit(1)
	}
}

func run(lockPath, importerName, store, ref, out string) error {
	lock, err := loadLockfile(lockPath, importerName)
	if err != nil {
		return err
	}

	keys := lock.closure(importerName)
	components := make([]cyclonedx.Component, 0, len(keys))

	for _, key := range keys {
		name, version := splitKey(key)
		if name == "" || version == "" {
			continue
		}

		// A link: or file: dependency has no registry tarball and no entry
		// here; it is part of the workspace, not of what it ships.
		pkg, ok := lock.Packages[name+"@"+version]
		if !ok {
			continue
		}

		// A package built for another platform is in the lockfile and not on
		// disk, so the document describes the tree as installed — which is
		// what cyclonedx-npm did, and why the committed SBOM names a linux
		// TypeScript. Say so rather than drop it quietly.
		rendered, err := component(store, ref, name, version, pkg.Resolution.Integrity)
		if errors.Is(err, errNotInstalled) {
			fmt.Fprintf(os.Stderr, "sbomnpm: not installed here, omitted: %s@%s\n", name, version)

			continue
		}

		if err != nil {
			return err
		}

		components = append(components, rendered)
	}

	sort.Slice(components, func(i, j int) bool {
		return components[i].BOMRef < components[j].BOMRef
	})

	// Only the components are consumed: build-sbom.sh merges them into an
	// envelope of its own that carries no volatile fields.
	document := cyclonedx.BOM{
		BOMFormat:   "CycloneDX",
		SpecVersion: cyclonedx.SpecVersion1_6,
		Version:     1,
		Components:  &components,
	}

	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}

	encoded = append(encoded, '\n')

	if out == "" {
		_, err = os.Stdout.Write(encoded)

		return err
	}

	return os.WriteFile(out, encoded, 0o644)
}
