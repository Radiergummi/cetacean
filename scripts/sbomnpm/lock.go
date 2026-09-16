package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// pnpm writes a multi-document lockfile: the first records the package manager
// pinned by package.json#packageManager, the project's own graph follows. The
// importer we are asked about is what tells them apart.
type lockfile struct {
	Importers map[string]importer     `yaml:"importers"`
	Packages  map[string]lockPackage  `yaml:"packages"`
	Snapshots map[string]lockSnapshot `yaml:"snapshots"`
}

type importer struct {
	Dependencies map[string]importerDep `yaml:"dependencies"`
}

type importerDep struct {
	Version string `yaml:"version"`
}

type lockPackage struct {
	Resolution struct {
		Integrity string `yaml:"integrity"`
	} `yaml:"resolution"`
}

type lockSnapshot struct {
	Dependencies         map[string]string `yaml:"dependencies"`
	OptionalDependencies map[string]string `yaml:"optionalDependencies"`
}

// loadLockfile returns the document describing importer, so the package
// manager's own document cannot be mistaken for the project's.
func loadLockfile(path, want string) (*lockfile, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)

	for {
		var doc lockfile
		if err := decoder.Decode(&doc); err != nil {
			return nil, fmt.Errorf("no document in %s declares importer %q: %w", path, want, err)
		}

		if _, ok := doc.Importers[want]; ok {
			return &doc, nil
		}
	}
}

// closure walks the production graph from one importer's direct dependencies.
// Development dependencies are absent from the importer entry we read, so they
// never enter; optional dependencies do, because an installed one ships.
func (l *lockfile) closure(imp string) []string {
	seen := map[string]bool{}
	queue := []string{}

	for name, dep := range l.Importers[imp].Dependencies {
		queue = append(queue, name+"@"+dep.Version)
	}

	for len(queue) > 0 {
		key := queue[len(queue)-1]
		queue = queue[:len(queue)-1]

		if seen[key] {
			continue
		}

		seen[key] = true

		snapshot, ok := l.Snapshots[key]
		if !ok {
			continue
		}

		for _, deps := range []map[string]string{snapshot.Dependencies, snapshot.OptionalDependencies} {
			for name, version := range deps {
				queue = append(queue, name+"@"+version)
			}
		}
	}

	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}

	return keys
}

// splitKey separates a snapshot key into its package name and version. Keys
// carry the resolved peers in parentheses and the name may itself be scoped,
// so the version is what follows the last @ before that suffix.
func splitKey(key string) (name, version string) {
	if cut := strings.IndexByte(key, '('); cut >= 0 {
		key = key[:cut]
	}

	at := strings.LastIndexByte(key, '@')
	if at <= 0 {
		return key, ""
	}

	return key[:at], key[at+1:]
}
