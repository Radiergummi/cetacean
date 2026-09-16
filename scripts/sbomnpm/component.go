package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cyclonedx "github.com/CycloneDX/cyclonedx-go"
)

// errNotInstalled reports a package the lockfile names and the store does not
// hold, which is ordinary for a dependency built for another platform.
var errNotInstalled = errors.New("not present in the store")

// packageManifest is the part of a package's own package.json that reaches the
// SBOM. Author and repository are polymorphic in the wild, so both are decoded
// loosely and normalised below.
type packageManifest struct {
	Description string          `json:"description"`
	Homepage    string          `json:"homepage"`
	License     json.RawMessage `json:"license"`
	Author      json.RawMessage `json:"author"`
	Repository  json.RawMessage `json:"repository"`
	Bugs        json.RawMessage `json:"bugs"`
}

// storeDir locates the unpacked package inside pnpm's virtual store. A store
// entry is named for the package and version with `/` replaced by `+`, and
// carries a suffix naming the peers it was resolved against.
func storeDir(store, name, version string) (string, error) {
	mangled := strings.ReplaceAll(name, "/", "+") + "@" + version

	entries, err := os.ReadDir(store)
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if entry.Name() != mangled && !strings.HasPrefix(entry.Name(), mangled+"_") {
			continue
		}

		dir := filepath.Join(store, entry.Name(), "node_modules", filepath.FromSlash(name))
		if _, err := os.Stat(dir); err == nil {
			return dir, nil
		}
	}

	return "", fmt.Errorf("%s@%s: %w", name, version, errNotInstalled)
}

// component renders one package as a CycloneDX component in the shape the
// licenses page and the license-text harvester read back.
func component(store, ref, name, version, integrity string) (cyclonedx.Component, error) {
	dir, err := storeDir(store, name, version)
	if err != nil {
		return cyclonedx.Component{}, err
	}

	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return cyclonedx.Component{}, err
	}

	var manifest packageManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return cyclonedx.Component{}, fmt.Errorf("%s@%s: %w", name, version, err)
	}

	group, bare := "", name
	if scope, rest, ok := strings.Cut(name, "/"); ok {
		group, bare = scope, rest
	}

	out := cyclonedx.Component{
		BOMRef:      ref + "|" + name + "@" + version,
		Type:        cyclonedx.ComponentTypeLibrary,
		Name:        bare,
		Group:       group,
		Version:     version,
		Description: manifest.Description,
		Author:      personName(manifest.Author),
		PackageURL:  purl(group, bare, version),
	}

	if licenses := licenses(manifest.License); licenses != nil {
		out.Licenses = licenses
	}

	refs := externalRefs(&manifest, name, bare, version, integrity)
	if len(refs) > 0 {
		out.ExternalReferences = &refs
	}

	return out, nil
}

func purl(group, bare, version string) string {
	if group == "" {
		return "pkg:npm/" + bare + "@" + version
	}

	// The scope's leading @ is escaped; the separator between scope and name
	// is not. Anything else fails to round-trip through a purl parser.
	return "pkg:npm/%40" + strings.TrimPrefix(group, "@") + "/" + bare + "@" + version
}

// licenses renders package.json's `license`, which is an SPDX identifier, an
// SPDX expression, or (long deprecated, still in the wild) an object.
func licenses(raw json.RawMessage) *cyclonedx.Licenses {
	if len(raw) == 0 {
		return nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		var object struct {
			Type string `json:"type"`
		}

		if json.Unmarshal(raw, &object) != nil || object.Type == "" {
			return nil
		}

		text = object.Type
	}

	if text == "" {
		return nil
	}

	if strings.ContainsAny(text, " ") {
		return &cyclonedx.Licenses{{Expression: text}}
	}

	return &cyclonedx.Licenses{{License: &cyclonedx.License{ID: text}}}
}

// personName reads npm's people field, which is either a string in
// "name <email> (url)" form or an object carrying the same parts.
func personName(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if cut := strings.IndexAny(text, "<("); cut >= 0 {
			text = text[:cut]
		}

		return strings.TrimSpace(text)
	}

	var object struct {
		Name string `json:"name"`
	}

	_ = json.Unmarshal(raw, &object)

	return object.Name
}

func externalRefs(
	m *packageManifest,
	name, bare, version, integrity string,
) []cyclonedx.ExternalReference {
	refs := []cyclonedx.ExternalReference{}

	if m.Homepage != "" {
		refs = append(refs, cyclonedx.ExternalReference{
			URL:  m.Homepage,
			Type: cyclonedx.ERTypeWebsite,
		})
	}

	if url := urlField(m.Repository); url != "" {
		refs = append(refs, cyclonedx.ExternalReference{URL: url, Type: cyclonedx.ERTypeVCS})
	}

	if url := urlField(m.Bugs); url != "" {
		refs = append(
			refs,
			cyclonedx.ExternalReference{URL: url, Type: cyclonedx.ERTypeIssueTracker},
		)
	}

	// The tarball and its hash are the supply-chain half of the document: the
	// lockfile is the only place that records what was actually fetched.
	if hashes := integrityHashes(integrity); hashes != nil {
		refs = append(refs, cyclonedx.ExternalReference{
			URL:    "https://registry.npmjs.org/" + name + "/-/" + bare + "-" + version + ".tgz",
			Type:   cyclonedx.ERTypeDistribution,
			Hashes: hashes,
		})
	}

	return refs
}

// urlField reads repository/bugs, each of which is a string or an object with
// a url.
func urlField(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}

	var object struct {
		URL string `json:"url"`
	}

	_ = json.Unmarshal(raw, &object)

	return object.URL
}

// integrityHashes converts npm's subresource integrity to CycloneDX's, which
// wants the digest hex-encoded and the algorithm spelled with a dash.
func integrityHashes(integrity string) *[]cyclonedx.Hash {
	algorithm, encoded, ok := strings.Cut(integrity, "-")
	if !ok {
		return nil
	}

	digest, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil
	}

	var kind cyclonedx.HashAlgorithm

	switch algorithm {
	case "sha512":
		kind = cyclonedx.HashAlgoSHA512
	case "sha256":
		kind = cyclonedx.HashAlgoSHA256
	case "sha1":
		kind = cyclonedx.HashAlgoSHA1
	default:
		return nil
	}

	return &[]cyclonedx.Hash{{Algorithm: kind, Value: hex.EncodeToString(digest)}}
}
