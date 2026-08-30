package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
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

		licenses, found, err := readTexts(dir, licenseStems)
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

		entry := sbom.ComponentTexts{License: intern(joinTexts(licenses))}

		notices, hasNotice, err := readTexts(dir, noticeStems)
		if err != nil {
			return sbom.Artifact{}, err
		}

		if hasNotice {
			entry.Notice = intern(joinTexts(notices))
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
		dir, err := npmDir(roots.NodeModules, component.Name, component.Version)
		if err != nil {
			return "", false, err
		}

		return dir, true, nil
	default:
		return "", false, nil
	}
}

// npmDir locates the directory holding one specific version of an npm
// package. npm hoists a single version of any package to the top of
// node_modules and nests the rest under the dependents that need them, so the
// hoisted path names only the package, not the version — reading its
// package.json is what tells two installed versions apart. Attributing the
// hoisted version's license text to a different version listed in the SBOM
// would be silently wrong, and looking nowhere but the top level would fail
// outright on a package that only ever appears nested.
func npmDir(root, name, version string) (string, error) {
	relative := filepath.FromSlash(name)

	hoisted := filepath.Join(root, relative)
	if installedVersion(hoisted) == version {
		return hoisted, nil
	}

	nested := string(filepath.Separator) + filepath.Join("node_modules", relative)
	found := ""

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !entry.IsDir() || !strings.HasSuffix(path, nested) {
			return nil
		}

		if installedVersion(path) != version {
			return nil
		}

		found = path

		return fs.SkipAll
	})

	// A missing root is the same answer as an empty one: the package is not
	// installed, and the error below says so in the terms the caller can act
	// on.
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("search %s for %s@%s: %w", root, name, version, err)
	}

	if found == "" {
		return "", fmt.Errorf(
			"npm package %s@%s is not installed under %s — "+
				"run npm install before harvesting",
			name, version, root,
		)
	}

	return found, nil
}

// installedVersion reports the version an npm package directory declares, or
// "" when it holds no readable package.json.
func installedVersion(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return ""
	}

	var manifest struct {
		Version string `json:"version"`
	}

	if err := json.Unmarshal(data, &manifest); err != nil {
		return ""
	}

	return manifest.Version
}

// namedText is one license or notice file: its filename, used to label the
// text when a component ships more than one, and its normalized contents.
type namedText struct {
	name string
	text string
}

// readTexts returns every regular file in dir whose name starts with the first
// stem that matches anything (case-insensitively), with line endings
// normalized. os.ReadDir returns entries ordered by filename, so the selection
// does not depend on directory order.
//
// Every match for the winning stem is collected rather than only the first,
// because a dual-licensed package ships LICENSE-APACHE and LICENSE-MIT with no
// plain LICENSE and owes both texts; taking the alphabetically first would
// drop half the attribution with nothing to show for it. A missing dir is
// reported as no match, not an error — the caller turns that into the "no
// license file" error itself.
func readTexts(dir string, stems []string) ([]namedText, bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}

		return nil, false, fmt.Errorf("read dir %s: %w", dir, err)
	}

	for _, stem := range stems {
		var texts []namedText

		seen := map[string]bool{}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			name := entry.Name()

			if !strings.HasPrefix(strings.ToLower(name), stem) {
				continue
			}

			if !isLicenseCandidate(name) {
				continue
			}

			candidate := filepath.Join(dir, name)

			// os.Stat (not the DirEntry's own Info, which does not follow
			// symlinks) so a symlinked license file is still picked up, same
			// as the previous filepath.Glob + os.Stat implementation.
			info, err := os.Stat(candidate)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}

			if info.Size() > maxTextBytes {
				return nil, false, fmt.Errorf(
					"%s is %d bytes, over the %d-byte cap — it is unlikely to be a license",
					candidate, info.Size(), maxTextBytes,
				)
			}

			text, err := readText(candidate)
			if err != nil {
				return nil, false, err
			}

			// A package shipping LICENSE and LICENSE.md with the same bytes
			// owes the text once, not twice.
			if seen[text] {
				continue
			}

			seen[text] = true
			texts = append(texts, namedText{name: name, text: text})
		}

		if len(texts) > 0 {
			return texts, true, nil
		}
	}

	return nil, false, nil
}

// readText reads one license or notice file, rejecting anything that is not
// text and normalizing CRLF so the artifact hashes the same wherever it is
// generated.
func readText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	if !utf8.Valid(data) {
		return "", fmt.Errorf("%s is not valid UTF-8", path)
	}

	return strings.ReplaceAll(string(data), "\r\n", "\n"), nil
}

// joinTexts renders one component's collected texts into the single string the
// pool holds. A lone text is returned untouched — the overwhelmingly common
// case, and the one whose pooled bytes must stay identical to what the
// committed artifact already carries. Several are labelled with the file they
// came from, because a reader owed two licenses needs to know which is which.
func joinTexts(texts []namedText) string {
	if len(texts) == 1 {
		return texts[0].text
	}

	var out strings.Builder

	for i, text := range texts {
		if i > 0 {
			out.WriteString("\n")
		}

		out.WriteString(text.name + "\n")
		out.WriteString(strings.Repeat("-", len(text.name)) + "\n\n")
		out.WriteString(strings.TrimRight(text.text, "\n") + "\n")
	}

	return out.String()
}
