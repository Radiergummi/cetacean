#!/usr/bin/env bash
# Generates the CycloneDX SBOM embedded into the binary and consumed by the
# open-source licenses page. Covers what the cetacean binary imports (Go, via
# `cyclonedx-gomod app`) plus frontend npm production dependencies. The two
# CycloneDX documents are merged with jq into a fresh envelope with no volatile
# fields (serialNumber, timestamp) so the committed file only changes when
# dependencies change.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="$repo_root/internal/api/sbom/sbom.cdx.json"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

version="$(git -C "$repo_root" describe --tags --abbrev=0 2>/dev/null || echo 0.0.0)"

command -v jq >/dev/null 2>&1 || {
  echo "error: 'jq' not found (system prerequisite). Install: https://jqlang.github.io/jq/" >&2
  exit 1
}

echo "==> Go modules (binary imports only)"
( cd "$repo_root" && go tool cyclonedx-gomod app -main . -json -licenses -output "$tmp/go.cdx.json" . )

echo "==> npm packages (production only)"
( cd "$repo_root/frontend" && npx --no-install cyclonedx-npm --omit dev --output-file "$tmp/npm.cdx.json" )

echo "==> merge + strip volatile fields (deterministic output)"
jq -s --arg version "$version" '
  {
    bomFormat: "CycloneDX",
    specVersion: (.[0].specVersion // "1.6"),
    version: 1,
    metadata: { component: { type: "application", name: "cetacean", version: $version } },
    components: ((.[0].components // []) + (.[1].components // []))
  }
' "$tmp/go.cdx.json" "$tmp/npm.cdx.json" > "$out"

echo "wrote $out"
