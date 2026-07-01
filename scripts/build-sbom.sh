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

command -v jq >/dev/null 2>&1 || {
  echo "error: 'jq' not found (system prerequisite). Install: https://jqlang.github.io/jq/" >&2
  exit 1
}

# `cyclonedx-gomod app` compiles the main package, which requires the embedded
# frontend (`//go:embed frontend/dist/*`). Fail early with a clear message
# instead of a confusing embed compile error when the frontend isn't built.
if [ -z "$(ls -A "$repo_root/frontend/dist" 2>/dev/null)" ]; then
  echo "error: frontend/dist is empty or missing. Run 'make build' (or 'cd frontend && npm run build') first." >&2
  exit 1
fi

echo "==> Go modules (binary imports only)"
# cyclonedx-gomod embeds GOOS/GOARCH into every purl and selects
# platform-specific imports (netlink, dbus, …) accordingly, so the output is
# host-dependent. Pin it to the shipped target (linux/amd64, the Docker
# `scratch` image) so generation is reproducible on any developer machine and
# the CI sbom-check gate is stable. `go tool` can't be used with a cross GOOS
# (it would cross-compile the tool itself and fail to exec), so build a
# host-native tool binary from the pinned tool-directive version and run that.
toolbin="$tmp/cyclonedx-gomod"
# -mod=readonly: the tool version is pinned via go.mod's tool directive, so
# building it must not mutate go.mod/go.sum as a side-effect (keeps `make sbom`
# from dirtying module files and fails fast if the module graph is incomplete).
( cd "$repo_root" && go build -mod=readonly -o "$toolbin" github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod )
( cd "$repo_root" && GOOS=linux GOARCH=amd64 "$toolbin" app -main . -json -licenses -output "$tmp/go.cdx.json" . )

echo "==> npm packages (production only)"
( cd "$repo_root/frontend" && npx --no-install cyclonedx-npm --omit dev --output-file "$tmp/npm.cdx.json" )

echo "==> merge + strip volatile fields (deterministic output)"
# Version is intentionally omitted from the envelope so the committed file
# only diffs when dependencies change, not when a release tag is cut.
jq -s '
  {
    bomFormat: "CycloneDX",
    specVersion: (.[0].specVersion // "1.6"),
    version: 1,
    metadata: { component: { type: "application", name: "cetacean" } },
    components: ((.[0].components // []) + (.[1].components // []))
  }
' "$tmp/go.cdx.json" "$tmp/npm.cdx.json" > "$out"

echo "wrote $out"
