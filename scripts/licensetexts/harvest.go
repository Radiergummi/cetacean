package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/radiergummi/cetacean/internal/api/sbom"
	"golang.org/x/mod/module"
)

// maxTextBytes caps a single license or notice file. Every real one is a few
// kilobytes; anything larger is a misidentified file, and embedding it would
// bloat the binary for no attribution value.
const maxTextBytes = 1 << 20

// licenseStems and noticeStems are the filename prefixes to search for,
// matched case-insensitively, in order. The first stem with any match wins,
// so the more canonical spellings come first. npm packages commonly ship a
// lowercase "license" file, so the match must not depend on case.
var (
	licenseStems = []string{"license", "licence", "copying"}
	noticeStems  = []string{"notice"}
)

// rejectedExtensions marks file extensions that mark a stem match as source
// code or build metadata rather than a license text — e.g. "license_test.go"
// sitting beside the real "LICENSE" file. Matched case-insensitively against
// the name's extension after the final dot; a name with no dot (like
// "LICENSE" or "LICENSE-MIT") is never rejected.
var rejectedExtensions = map[string]bool{
	"go": true, "js": true, "mjs": true, "cjs": true, "ts": true, "tsx": true,
	"jsx": true, "json": true, "yaml": true, "yml": true, "toml": true,
	"lock": true, "sum": true, "mod": true,
}

// isLicenseCandidate reports whether name is plausibly a license/notice text
// file rather than source or build metadata that merely shares a stem.
func isLicenseCandidate(name string) bool {
	ext := strings.TrimPrefix(filepath.Ext(name), ".")

	return !rejectedExtensions[strings.ToLower(ext)]
}

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
		dir, ok, err := componentDir(component, roots)
		if err != nil {
			return sbom.Artifact{}, err
		}

		if !ok {
			// An ecosystem with no package store to read from. It keeps its
			// inventory entry on the page; there is simply no text to attach.
			continue
		}

		license, found, err := readFirst(dir, licenseStems)
		if err != nil {
			return sbom.Artifact{}, err
		}

		if !found {
			return sbom.Artifact{}, fmt.Errorf(
				"no license file for %s %s (looked in %s for %s) — "+
					"add a mapping or vendor the text before shipping it",
				component.Name, component.Version, dir, strings.Join(licenseStems, ", "),
			)
		}

		entry := sbom.ComponentTexts{License: intern(license)}

		notice, hasNotice, err := readFirst(dir, noticeStems)
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
func componentDir(component sbom.Component, roots Roots) (string, bool, error) {
	switch component.Ecosystem {
	case "go":
		// The module cache encodes an uppercase letter as "!" plus its
		// lowercase form so a case-insensitive filesystem cannot collide two
		// distinct module paths. EscapePath is the canonical encoder, and it
		// rejects a path that is not a valid module path at all.
		escaped, err := module.EscapePath(component.Name)
		if err != nil {
			return "", false, fmt.Errorf("escape module path %s: %w", component.Name, err)
		}

		return filepath.Join(roots.GoModCache, escaped+"@"+component.Version), true, nil
	case "npm":
		return filepath.Join(roots.NodeModules, filepath.FromSlash(component.Name)), true, nil
	default:
		return "", false, nil
	}
}

// readFirst returns the contents of the first regular file in dir whose name
// starts with one of the stems (case-insensitively), with line endings
// normalized. os.ReadDir returns entries ordered by filename, so the choice
// does not depend on directory order. A missing dir is reported as no match,
// not an error — the caller turns that into the "no license file" error itself.
func readFirst(dir string, stems []string) (string, bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}

		return "", false, fmt.Errorf("read dir %s: %w", dir, err)
	}

	var path string

	for _, stem := range stems {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			if !strings.HasPrefix(strings.ToLower(entry.Name()), stem) {
				continue
			}

			if !isLicenseCandidate(entry.Name()) {
				continue
			}

			candidate := filepath.Join(dir, entry.Name())

			// os.Stat (not the DirEntry's own Info, which does not follow
			// symlinks) so a symlinked license file is still picked up, same
			// as the previous filepath.Glob + os.Stat implementation.
			info, err := os.Stat(candidate)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}

			if info.Size() > maxTextBytes {
				return "", false, fmt.Errorf(
					"%s is %d bytes, over the %d-byte cap — it is unlikely to be a license",
					candidate, info.Size(), maxTextBytes,
				)
			}

			path = candidate

			break
		}

		if path != "" {
			break
		}
	}

	if path == "" {
		return "", false, nil
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
