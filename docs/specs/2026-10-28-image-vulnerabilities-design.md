# Image vulnerabilities — design

For #5. Status: proposal. Builds on the image attestations of #539.

## Problem

Nothing in Cetacean tells you a running image has known vulnerabilities. The issue proposed
asking a Trivy server over HTTP, like Prometheus. Trivy's server mode doesn't work that way:
the server holds the vulnerability database, and the *client* pulls the image and analyses
its layers, then sends the package inventory over Trivy's Twirp RPC. Cetacean would have to
be that client, which means embedding Trivy's image analysis (a large dependency tree and
registry pulls of every layer) or running the `trivy` binary.

## Decision: scan the SBOM, not the image

#539 already reads the SPDX SBOM BuildKit attaches to an image, by digest, anonymously, and
caches it. That SBOM is the package inventory Trivy's client would have produced. So:

1. Cetacean takes the SBOM it already has for an image digest.
2. It sends it to a scanner the operator runs, configured like Prometheus:
   `scanner.url` (unset = off) and `scanner.token`.
3. It caches the findings by image digest, refreshed on a TTL (`scanner.interval`, default
   6h) because the database changes even when the image does not, and on first sight of a
   new digest. One scan per digest across all services; a bounded worker pool, so a large
   cluster queues rather than floods.

The scanner is **Trivy in server mode**, called over its RPC with the SBOM-derived inventory
— to be confirmed in a spike against Trivy's `rpc/` protos before implementation; if that
proves too coupled to Trivy internals, the fallback is a small sidecar image running
`trivy sbom --server` and exposing a plain JSON endpoint, shipped in `compose.security.yaml`.

### Images without an SBOM

Most third-party images carry one now, but many private builds do not. Those show
"no SBOM, can't scan" with a link to how to add one (`docker buildx build --sbom=true`). We
do not fall back to pulling layers. That keeps Cetacean from becoming an image analyser,
and says plainly what wasn't checked.

### Surfaces

- Service and task image card: counts by severity, beside the attestation check, linking to
  a **Vulnerabilities** page in the same shape as the SBOM page: CVE, severity, package,
  installed and fixed version, searchable, CSV.
- `GET /services/{id}/vulnerabilities` (and under `/tasks/{id}`), MCP resource to match.
- A `vulnerable-image` recommendation for critical findings with a fixed version available,
  so the cluster-wide view is the recommendations page rather than a new one.

Like the attestations, findings are what the scanner says about what the image says about
itself, and the UI says so.

## Open for the spike

- Exact Trivy RPC calls and whether an SPDX inventory maps onto them without the OS release
  metadata a layer scan provides (OS package matching needs the distro and version).

## Out of scope

Scanning images that aren't running, policy enforcement, Grype/OSV backends (the scanner
client is one small interface, so adding one later is cheap).
