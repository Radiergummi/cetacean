# Image vulnerabilities — design

For #5. Status: proposal. Depends on the image attestations of #539, which is not merged; this
design is to be implemented after it.

## Problem

Nothing in Cetacean tells you a running image has known vulnerabilities. The issue proposed
asking a Trivy server over HTTP, like Prometheus. Trivy's server mode doesn't work that way:
the server holds the vulnerability database, and the *client* pulls the image and analyses
its layers, then sends the package inventory over Trivy's Twirp RPC. Cetacean would have to
be that client, which means embedding Trivy's image analysis (a large dependency tree and
registry pulls of every layer) or running the `trivy` binary.

## Decision: scan the SBOM, not the image

#539 reads the SPDX SBOM BuildKit attaches to an image, by digest. That SBOM is the package
inventory Trivy's client would have produced. So:

1. Cetacean fetches the SBOM of every running image digest, through #539's registry code.
2. It sends it to a scanner the operator runs, configured like Prometheus:
   `scanner.url` (unset = off) and `scanner.token`. Scanning needs
   `server.image_attestations` on.
3. It keeps the findings by image digest, rescanned on `scanner.interval` (default 6h) because
   the database changes even when the image does not, and on first sight of a new digest. One
   scan per digest across all services; a bounded worker pool, so a large cluster queues rather
   than floods.

### Coverage: public images only

#539 reaches registries anonymously and has no credentials. A private repository answers 401 or
403, so its images have no SBOM and are not scanned. In practice the scanner covers public images
— base images and third-party services — and most of an organisation's own images show
"private, not scanned" until registry credentials exist, which is a separate feature. The UI and
the API say which of the three it is: scanned, no SBOM, or private.

### Decision: the scanner is `trivy sbom` behind a plain endpoint

OS-package matching needs the distro and its version. A layer scan reads them from
`/etc/os-release`; an SPDX document carries them only as the `distro=` qualifier of each
package's purl, if the generator wrote one, and Trivy's RPC expects them as a separate field its
own analyser fills. Mapping SPDX onto that RPC would make Cetacean reimplement the part of
Trivy's client that knows the distro, against an internal protocol with no compatibility promise.

So the scanner contract is plain: `POST {scanner.url}` with the SPDX document as
`application/spdx+json`, answered with Trivy's JSON report. The reference implementation is a
small sidecar that runs `trivy sbom --server <trivy> --format json` on the body, shipped with a
Trivy server in `compose.security.yaml`. `trivy sbom` owns the SPDX reading, distro detection
included. Anything that speaks the contract can stand in for it.

#### Who ships the sidecar image

The sidecar is a few dozen lines around the `trivy` binary, built from this repository in the
release workflow and tagged with Cetacean's version, so the contract and its only implementation
move together. A documented Dockerfile for operators to build would keep the release pipeline to
one image, at the cost of every deployment owning a build.

### Fetching ahead of time

#539 fetches lazily, when a page asks, and keeps the raw SPDX only while it fits its fixed
64 MiB in-memory cache. A rescan of everything running cannot depend on that, so the scanner
fetches for itself:

- **What.** For every distinct image digest a running task uses, the SBOM of each platform the
  cluster's nodes run, not every platform in the index. It uses #539's registry client, so the
  token cache and the per-digest failure cache are shared rather than duplicated.
- **Where it keeps them.** SBOMs are immutable per digest, so they are kept on disk under
  `storage.data_dir/sbom/`, bounded by `scanner.sbom_cache` (default 512 MiB), evicting the
  least recently scanned. They survive restarts, so a restart costs no registry requests, and a
  rescan costs none either: it resends what is on disk. An evicted SBOM is fetched again when
  next needed. Findings are small and kept in memory, keyed by digest and platform.
- **Registry budget.** Cetacean runs on a manager node, so on Docker Hub its anonymous pulls
  count against the same per-IP limit the nodes pull images from. Only manifest GETs count; the
  SBOM blob does not. A new digest therefore costs one index GET plus one attestation manifest
  per platform in use, once. The prefetch:
  - fetches at most two digests per registry at a time;
  - reads Docker Hub's `ratelimit-remaining` header and pauses fetching from Docker Hub while it
    is below a quarter of `ratelimit-limit`, leaving the rest for deployments;
  - on 429 waits out `Retry-After`, or #539's one-minute failure retry when there is none.
  An image waiting on the budget shows "not scanned yet", not "clean".

### Images without an SBOM

Many private builds carry none, and some public images don't. Those show "no SBOM, can't scan"
with a link to how to add one (`docker buildx build --sbom=true`). We do not fall back to pulling
layers. That keeps Cetacean from becoming an image analyser, and says plainly what wasn't checked.

### Surfaces

- Service and task image card: counts by severity, beside the attestation check, linking to
  a **Vulnerabilities** page in the same shape as the SBOM page: CVE, severity, package,
  installed and fixed version, searchable, CSV.
- `GET /services/{id}/vulnerabilities` (and under `/tasks/{id}`), MCP resource to match.
- A `vulnerable-image` recommendation for critical findings with a fixed version available,
  so the cluster-wide view is the recommendations page rather than a new one.

Like the attestations, findings are what the scanner says about what the image says about
itself, and the UI says so.

## Out of scope

Scanning images that aren't running, private registries and their credentials, policy
enforcement, Grype/OSV backends (the scanner contract is one HTTP call, so adding one later is
cheap).
