package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/radiergummi/cetacean/internal/api/sbom"
)

// maxTextBytes caps a single license or notice file. Every real one is a few
// kilobytes; anything larger is a misidentified file, and embedding it would
// bloat the binary for no attribution value.
const maxTextBytes = 1 << 20

// licenseNames and noticeNames are the filename globs to search, in order.
// The first match wins, so the more canonical spellings come first.
var (
	licenseNames = []string{"LICENSE*", "LICENCE*", "COPYING*"}
	noticeNames  = []string{"NOTICE*"}
)

// Roots locates the two package stores the harvester reads from. Tests point
// these at fixtures; the CLI points them at the real module cache and
// node_modules.
type Roots struct {
	GoModCache  string
	NodeModules string
}

// Harvest resolves every Go and npm component to its source directory and
// collects its license and notice text into a deduplicated pool.
func Harvest(doc sbom.Document, roots Roots) (sbom.Artifact, error) {
	artifact := sbom.Artifact{
		Texts:      map[string]string{},
		Components: map[string]sbom.ComponentTexts{},
	}

	intern := func(text string) string {
		sum := sha256.Sum256([]byte(text))
		id := hex.EncodeToString(sum[:16])
		artifact.Texts[id] = text

		return id
	}

	for _, component := range doc.Components {
		dir, ok := componentDir(component, roots)
		if !ok {
			// An ecosystem with no package store to read from. It keeps its
			// inventory entry on the page; there is simply no text to attach.
			continue
		}

		license, found, err := readFirst(dir, licenseNames)
		if err != nil {
			return sbom.Artifact{}, err
		}

		if !found {
			return sbom.Artifact{}, fmt.Errorf(
				"no license file for %s %s (looked in %s for %s) — "+
					"add a mapping or vendor the text before shipping it",
				component.Name, component.Version, dir, strings.Join(licenseNames, ", "),
			)
		}

		entry := sbom.ComponentTexts{License: intern(license)}

		notice, hasNotice, err := readFirst(dir, noticeNames)
		if err != nil {
			return sbom.Artifact{}, err
		}

		if hasNotice {
			entry.Notice = intern(notice)
		}

		artifact.Components[sbom.ComponentKey(component)] = entry
	}

	return artifact, nil
}

// componentDir maps a component to the directory its sources were unpacked
// into. Reports false for ecosystems with no local package store.
func componentDir(component sbom.Component, roots Roots) (string, bool) {
	switch component.Ecosystem {
	case "go":
		return filepath.Join(
			roots.GoModCache,
			escapeModulePath(component.Name)+"@"+component.Version,
		), true
	case "npm":
		return filepath.Join(roots.NodeModules, filepath.FromSlash(component.Name)), true
	default:
		return "", false
	}
}

// escapeModulePath applies the Go module cache's case encoding: an uppercase
// letter becomes "!" followed by its lowercase form, so that case-insensitive
// filesystems cannot collide two distinct module paths.
func escapeModulePath(module string) string {
	var out strings.Builder

	for _, r := range module {
		if r >= 'A' && r <= 'Z' {
			out.WriteByte('!')
			out.WriteRune(r + ('a' - 'A'))

			continue
		}

		out.WriteRune(r)
	}

	return out.String()
}

// readFirst returns the contents of the first regular file in dir matching any
// of the patterns, with line endings normalized. Matches are sorted so the
// choice does not depend on directory order.
func readFirst(dir string, patterns []string) (string, bool, error) {
	var matches []string

	for _, pattern := range patterns {
		hits, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return "", false, fmt.Errorf("glob %s in %s: %w", pattern, dir, err)
		}

		for _, hit := range hits {
			info, err := os.Stat(hit)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}

			matches = append(matches, hit)
		}

		if len(matches) > 0 {
			break
		}
	}

	if len(matches) == 0 {
		return "", false, nil
	}

	slices.Sort(matches)
	path := matches[0]

	info, err := os.Stat(path)
	if err != nil {
		return "", false, fmt.Errorf("stat %s: %w", path, err)
	}

	if info.Size() > maxTextBytes {
		return "", false, fmt.Errorf(
			"%s is %d bytes, over the %d-byte cap — it is unlikely to be a license",
			path, info.Size(), maxTextBytes,
		)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("read %s: %w", path, err)
	}

	if !utf8.Valid(data) {
		return "", false, fmt.Errorf("%s is not valid UTF-8", path)
	}

	// Normalize CRLF so the artifact hashes the same wherever it is generated.
	return strings.ReplaceAll(string(data), "\r\n", "\n"), true, nil
}
