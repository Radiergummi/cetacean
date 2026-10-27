# Image attestations — design

## Problem

A service page says which image runs, but not where it came from or what is inside it. Many images
already carry the answer: BuildKit attaches provenance and an SBOM to an image when it pushes it.
Since buildx 0.10, minimal provenance is on by default, and the Docker Official Images ship both.
Finding that data today means running `docker buildx imagetools inspect` against the registry, by
hand, one image at a time.

Cetacean can show it next to the image. That answers "which commit is this?", "who built it?" and
"what is in it?" without leaving the dashboard.

## Goals

- Show a service's build provenance for each platform its image has: the source repository and
  commit, the builder, when it was built, the Dockerfile, and the base images it was built from.
- For an image built on GitHub Actions, say who triggered the build, from which ref and workflow,
  and link to the run.
- When the provenance embeds the Dockerfile, show it with each line annotated with the image
  layers it produced and their sizes. This answers "why is this image so big?".
- Show a task's provenance for the platform of the node it runs on.
- Show an image's SBOM as a package table that can be searched and downloaded. For each package,
  show where it was found in the image.
- Expose the same data over REST and MCP.

## Non-goals

- **Verifying anything.** Checking signatures (cosign, Notation) needs a trust policy that
  Cetacean doesn't have. What is shown is what the image claims about itself.
- **The OCI 1.1 referrers API.** Cosign attestations and other artifacts that refer to an image are
  stored outside the image index. v1 reads only the attestations BuildKit stores inside the index.
- **Private registries.** Registries are reached anonymously, the same way the update check reaches
  them. Supporting credentials would be its own feature.
- **CycloneDX, or SBOMs from other generators.** v1 reads only BuildKit's SPDX JSON.
- **Vulnerability matching.** That needs a vulnerability database. Cetacean supplies the package
  list; scanning it is for the tools the SPDX download feeds.
- **Searching packages across services** ("which services ship log4j 2.14?"). That needs every
  SBOM fetched ahead of time. It is the obvious follow-up, and the cache below is designed so that
  follow-up can reuse it.

## Anti-goals

- **Anything that reads as verified.** That rules out checkmarks, "verified" badges and green
  shields. The section says "as recorded by the image's build" or words to that effect.
- **Fetching attestations in the background scan.** An SBOM can run to several megabytes. All
  fetches are on demand.
- **Contacting a registry on behalf of a caller who cannot read the service or task.** The ACL check
  comes before any network request.
- **Guessing.** An image that is not pinned by digest shows nothing. Resolving its tag again could
  name a different image from the one that is running.

## Where the data is

Swarm pins a service image as `name:tag@<digest>`, where the digest is normally that of the image
index. A task's spec carries the same pinned reference, so during a rolling update each task names
its own image.

An image deployed as `name@<digest>`, with no tag, is pinned as well, and is in scope.
`imagecheck.ParsePinned` rejects that form because it has no tag to check, so this feature needs
its own parser. The update check keeps rejecting the form.

When BuildKit pushes an image with attestations, the index gains one extra manifest per platform:

- The index entry has `platform: {os: unknown, architecture: unknown}` and the annotations
  `vnd.docker.reference.type: attestation-manifest` and `vnd.docker.reference.digest: <digest of
  the platform manifest it describes>`.
- Its layers are in-toto statements (`application/vnd.in-toto+json`). Each layer's annotation
  `in-toto.io/predicate-type` names what it holds:
  - `https://spdx.dev/Document`: the SBOM.
  - `https://slsa.dev/provenance/v0.2` or `https://slsa.dev/provenance/v1`: the provenance.

The platform an attestation belongs to is the platform of the index entry its
`vnd.docker.reference.digest` names. A single-platform image pushed with attestations is still an
index, so the same path covers it. An image without attestations is either a plain manifest or an
index with no attestation entries, and both mean "none".

Every manifest and blob is addressed by digest, so nothing fetched for a digest ever changes.
This includes the answer "this index has no attestations".

## Fetching

### Registry client

The existing anonymous client in `internal/imagecheck` already handles the hard parts: host
resolution, Bearer challenges, token caching, HTTPS-only token realms and digest verification.
It gains two methods:

- **Get a manifest by digest.** It sends the same `Accept` list as now, plus the OCI manifest
  type, and rejects a body whose digest differs from the one requested.
- **Get a blob by digest, with a size cap.** It also verifies the digest of what it read.

The package needs a digest-addressed form of `Reference`, since today's only has a tag. How to
spell it is the builder's call. Attestations themselves get a new package, `internal/attestations`:
it walks the index, parses the predicates and owns the cache.

### Requests per image

| Fetch | When | Docker Hub pulls |
|---|---|---|
| Index (GET by digest) | First look at an image | 1 |
| One attestation manifest per platform | First look at an image | 1 each |
| Provenance blob, per platform | First look at an image | 0 (blob) |
| SBOM blob, one platform | When the SBOM is opened | 0 (blob) |
| Platform manifest, one platform | When the Dockerfile view is opened | 1 |

On Docker Hub only manifest GETs count against the anonymous pull limit. With the cache, an image
costs 1 + platforms pulls once, for as long as it stays in the cache.

### Cache

- The cache is keyed by digest and holds parsed results, not raw bodies. The exception is the raw
  SPDX document, which the download needs. It is kept only while it is within the size cap.
- It is bounded by bytes, evicting least recently used. 64 MiB by default is my call. It is not a
  setting.
- Answers about content never expire: "these attestations", "no attestations" and "SBOM too
  large".
- Failures are cached for one minute, then retried: network errors, 5xx and 429.
- Two answers are cached for an hour, the same interval the update check uses:
  - "Private" (401 or 403), because a repository can become public.
  - "Missing" (404 on the pinned digest), because the registry may have deleted or garbage-collected
    the image. Containers that are already running are unaffected, but nothing can be read about it.
- Fetches are de-duplicated per digest, so ten viewers opening the same page cause one fetch.

### Limits

- Manifests use the client's existing manifest size cap.
- A provenance blob is capped at 4 MiB. An SBOM blob is capped at 32 MiB, which is my call.
  BuildKit's SBOMs for large images are a few MB, so this leaves wide margin. The cap is checked
  against the size in the layer descriptor before any download, and again while reading.
- Each registry request has a 10 s timeout, and the whole operation for one image has 30 s.

## What is extracted

Reading the formats is `internal/attestations`' job. It knows OCI, SLSA and SPDX, and nothing
about Swarm. What a service or a task shows, which means choosing the image, the platform and the
links, lives in `internal/cluster`, because both transports render it.

### Provenance

Both SLSA v0.2 and v1 are read into one summary. Which fields are present depends on the build's
provenance mode: `mode=min`, the default, has fewer. A field the predicate does not carry is
absent, not empty.

The field paths below were checked against two real images:

- `nginx:latest`, an official image with v0.2 provenance;
- `docker/buildkit-syft-scanner:latest`, built by GitHub Actions with v1 `mode=max` provenance.

Both go into the test fixtures.

| Field | v0.2 | v1 |
|---|---|---|
| Config source (`uri`, digest, Dockerfile path) | `invocation.configSource` (`uri`, `digest`, `entryPoint`) | `buildDefinition.externalParameters.configSource` (`uri`, `digest`, `path`) |
| VCS fallback | BuildKit metadata `vcs.source`, `vcs.revision` | same |
| Builder | `builder.id` | `runDetails.builder.id` |
| Started, finished | `metadata.buildStartedOn`, `buildFinishedOn` | `runDetails.metadata.startedOn`, `finishedOn` |
| Frontend | `invocation.parameters.frontend` | `buildDefinition.externalParameters.request.frontend` |
| Base images and other inputs | `materials` | `buildDefinition.resolvedDependencies` |
| Reproducible | `metadata.reproducible` | absent |

BuildKit metadata sits under a different key in each version:

- v0.2: `metadata["https://mobyproject.org/buildkit@v1#metadata"]`
- v1: `runDetails.metadata.buildkit_metadata`

- **Source and revision.** These come from the config source first.
  - A git context records `uri` as `<repo>.git#<ref or commit>[:<subdir>]` and the commit in
    `digest.sha1`. The summary splits that into repository, ref, commit and subdirectory.
  - Only when the context was not git does the summary fall back to BuildKit's `vcs` metadata,
    which buildx records for a build from a local checkout.
  - In that case `configSource` holds only `path`, and `vcs` carries `source` (the `origin`
    remote's URL) and `revision` (the commit). A probe build confirmed this.
  - The remote URL is shown only after it passes the safe-URL rule, and any userinfo is stripped
    from it.
- **Inputs** are listed as their `uri` and digest. A `pkg:docker/...` purl is shown as the image
  reference it encodes.
- **Build arguments are not part of the summary**, though `mode=max` records them. They are still
  in the raw predicate. The raw predicate is no new exposure: anyone who can pull the image
  anonymously can read it.
- **Links:**
  - A source on `github.com`, `gitlab.com` or `codeberg.org` links the repository and the revision
    to the host's commit page.
  - Any other source is plain text.
  - A builder ID that is an `https` URL is a link. `docker/build-push-action` sets it to the
    workflow run.
  - This is the same safe-URL rule the `cetacean.link.*` labels use.

### CI context

When buildx runs on GitHub Actions in `mode=max`, it records the job's `github_*` variables. In
v1 they are in `buildDefinition.internalParameters`. Where v0.2 puts them is unverified, and the
builder confirms it against a v0.2 `mode=max` fixture before reading them there.

The summary takes:

| Field | From |
|---|---|
| Actor | `github_actor` (and `github_triggering_actor` when it differs) |
| Event | `github_event_name` |
| Ref | `github_ref` |
| Workflow | `github_workflow_ref` (`<repo>/<path>@<ref>`) and `github_workflow_sha` |
| Run | `github_server_url`, `github_repository`, `github_run_id`, `github_run_attempt` |
| Runner | `github_runner_environment` (`github-hosted` or `self-hosted`) |

- **Run link:**
  `{github_server_url}/{github_repository}/actions/runs/{github_run_id}/attempts/{github_run_attempt}`.
  It is built only when the server URL is `https` and passes the safe-URL rule.
- **Workflow link:** the workflow file at `github_workflow_sha`.
- **Not taken:**
  - `github_event_payload` is the whole webhook body, several KB. It is never part of any summary.
  - All the other `github_*` IDs.
- **Other CI systems:** BuildKit records no equivalent for them, so the section is absent.

The actor is a GitHub login. It is as public as the image it came with, so Cetacean shows it.

### Dockerfile and layers

`mode=max` provenance embeds the build's sources in BuildKit metadata. `source.infos[]` holds
entries with `filename`, `language` and base64 `data`. `source.locations` maps each LLB step ID
to line ranges in one of those sources: `sourceIndex`, which defaults to 0, picks the source. The
LLB step IDs are the ones in `buildConfig`, i.e. `internalParameters.buildConfig` in v1 and
`buildConfig` in v0.2.

`layers` maps `<step>:<output>` to the chains of layer descriptors that step's output consists
of, with digest and size.

The Dockerfile view joins these:

1. **The Dockerfile:** the first source whose `language` is `Dockerfile`, decoded.
2. **The layers:** fetched from the platform manifest. This is the one fetch the provenance cannot
   replace, because only the manifest says which layers the image is made of, in what order.
3. **Assigning layers to lines:**
   - Each manifest layer is assigned to a step whose `layers` chain contains its digest. Where
     several steps contain it, which happens when a step builds on the step before it, the
     assignment goes to the step whose own chain *ends* in that layer, since that step created it.
   - Base image layers therefore land on their `FROM` line.
   - A layer no step claims is listed as unattributed, not guessed.
   - Every layer-producing step in the stages that were built gets an entry. A probe build with
     `FROM`, then `RUN`, `RUN`, `COPY --from`, `COPY`, `RUN` in its final stage attributed all six
     manifest layers to their lines, and each step's chain extended the previous step's by
     exactly one layer. The syft-scanner sample has fewer entries only because its final stage
     is `scratch` plus one `COPY`.
   - A step can have several line ranges. Two stages `FROM` the same image share one source step,
     for example. The layers are shown against each range, but every total counts a layer once.
4. **The result:** each line range carries the layers it produced, with their sizes and a total.
   The view also shows the image's total size and how much of it came from the base image.

Only `mode=max` provenance has `source`. Without it, there is no Dockerfile view. The Dockerfile's
path is still shown from the config source, and still links to the repository when it can.

The embedded Dockerfile reveals nothing beyond what pulling the image already does: anyone who
can read it anonymously can read the provenance.

From the SPDX document:

- Every package except the ones the document describes, which stand for the image itself.
  - Syft names them through a `DESCRIBES` relationship from `SPDXRef-DOCUMENT`, not through
    `documentDescribes`. Both are honoured.
  - In the nginx SBOM the described package is a placeholder named `sbom`.
- For each package: name, version, ecosystem, license, purl and where it was found.
  - The ecosystem is the purl type: `deb`, `apk`, `npm`, `golang`, and so on. "unknown" when there
    is no purl.
  - The license is `licenseConcluded`, falling back to `licenseDeclared`. `NOASSERTION` and `NONE`
    become empty.
  - "Found in" is the package's `sourceInfo`, with Syft's fixed prefix `acquired package info
    from ` removed. Examples:
    - `go module information: /bin/syft-scanner`
    - `dpkg db: /var/lib/dpkg/status`

    The text is shown as is, not parsed further. Its wording belongs to Syft.
- Sorted by name, then version.
- A summary: the total count, the count per ecosystem, the number of packages with a license, and
  the generator (`creationInfo.creators`) and creation time.

License coverage depends on the ecosystem:

- Distro packages are well covered: 218 of nginx's 236 packages declare a license.
- Go modules are `NOASSERTION`, all 272 of them in the syft-scanner image.

The dashboard says this beside an empty license column, so that it does not read as "no licenses".

## Platform selection

The platform in a node description is reported the way `uname` names it (`x86_64`, `aarch64`).
It is normalized to OCI names (`amd64`, `arm64`) before matching. `normalizeArch` in
`internal/cluster/constraints.go` already does this, and it already filters out attestation
entries. Both are reused. The rules also live in `internal/cluster`:

- **Task:** the attestation for the platform of the task's node.
  - If that node's platform matches several index entries (`arm` with several variants, since the
    node description has no variant), all of them are returned.
  - If the task has no node yet, every platform is returned, the same as for a service.
- **Service:** every platform the index has. The dashboard selects first the platform most of the
  service's running tasks are on, and otherwise the first one listed.

## REST

All of these are read-only, need read access to the service or task, and are content-negotiated.
They carry JSON-LD and an ETag, which is strong because the content behind a digest never changes.

| Endpoint | Representations |
|---|---|
| `GET /services/{id}/provenance` | JSON |
| `GET /services/{id}/sbom?platform=os/arch[/variant]` | JSON, CSV, SPDX JSON, HTML (SPA) |
| `GET /tasks/{id}/provenance` | JSON |
| `GET /tasks/{id}/sbom?platform=os/arch[/variant]` | JSON, CSV, SPDX JSON, HTML (SPA) |
| `GET /services/{id}/dockerfile?platform=os/arch[/variant]` | JSON, `text/plain`, HTML (SPA) |
| `GET /tasks/{id}/dockerfile?platform=os/arch[/variant]` | JSON, `text/plain`, HTML (SPA) |

`/dockerfile`:

- **JSON** is the Dockerfile's lines with the layer assignments and size totals described above.
- **`text/plain`** is the Dockerfile itself.
- **`platform`** works as it does for `/sbom`.
- When the provenance embeds no source, the answer is a problem detail with its own type.

The `/provenance` platform entries say whether a Dockerfile view exists, so the dashboard links it
only when it does.

Provenance has no page of its own; it is a section of the detail page. Its endpoint therefore
refuses HTML, the way every endpoint refuses a type it cannot produce.

### `/provenance`

It always answers 200 with a `status` field when the service exists, so that the panel can show
each state:

| `status` | Meaning |
|---|---|
| `available` | An array of platforms, each with its manifest digest, the provenance summary (or none), and whether an SBOM exists, with its size and a `tooLarge` flag |
| `none` | The image has no attestations |
| `unpinned` | The image reference has no digest |
| `private` | The registry refused anonymous access |
| `missing` | The registry does not have the pinned digest, for example because it was deleted |
| `unavailable` | The registry could not be reached or failed. Carries `retryAfter` from the failure cache |
| `disabled` | The setting is off |

### `/sbom`

- **JSON** is the package table and summary from above.
- **CSV** has one row per package.
- **`application/spdx+json`** is the SPDX document as the registry stored it, extracted from its
  in-toto statement.
- **`platform`:**
  - It selects among the candidates. For a service, the candidates are every platform in the
    index. For a task, they are the platforms that match its node (see above).
  - With one candidate it may be left out. With several it is required.
  - A platform that is not among the candidates is a problem.
  - In the usual case a task has one candidate, so a task's SBOM link carries no parameter.
- Failures are RFC 9457 problem details, each with its own registered type:
  - no SBOM for that platform;
  - an ambiguous or unknown platform;
  - an SBOM over the size cap;
  - registry failure.

  Choosing status codes for these within RFC 9110 is the builder's call.

All four endpoints go into `api/openapi.yaml`, the JSON-LD context and `docs/api.md`.

## MCP

These resource templates mirror the REST endpoints:

- `cetacean://services/{id}/provenance`
- `cetacean://services/{id}/sbom{?platform}`
- `cetacean://tasks/{id}/provenance`
- `cetacean://tasks/{id}/sbom{?platform}`
- `cetacean://services/{id}/dockerfile{?platform}`
- `cetacean://tasks/{id}/dockerfile{?platform}`

The SBOM resource returns the JSON table, not raw SPDX. An agent asking about packages wants
rows, not a 5 MB document. The `cetacean://services/{id}` resource does not inline any of this,
so reading a service never contacts a registry.

## Dashboard

- **Service detail** gets a "Build" section next to the image. It shows:
  - a platform selector when there is more than one platform;
  - the provenance summary, with links as above;
  - when present, a CI line, such as "Pushed by crazy-max to `refs/tags/v1.12.1` · workflow
    `ci.yaml` · run 40359464897", with the run and workflow linked;
  - the package count per ecosystem, linking to the SBOM page;
  - a link to the Dockerfile page, when one exists.
- **Task detail** gets the same section for its own platform. When the task's image differs from
  the service's, which happens mid-update, that is visible because the task's digest is shown.
- **SBOM page** at `/services/:id/sbom` and `/tasks/:id/sbom`:
  - a searchable table with filters by ecosystem and license, and a "found in" column;
  - CSV and SPDX download buttons.
- **Dockerfile page** at `/services/:id/dockerfile` and `/tasks/:id/dockerfile`:
  - the Dockerfile, with syntax highlighting;
  - a gutter beside each instruction showing the size of the layers it produced, scaled so the
    heaviest lines stand out;
  - a header with the total size, the share that came from the base image, and any unattributed
    layers;
  - a raw download.
- **States:** each `status` gets a quiet one-line explanation, so "no attestations" reads as a
  fact about the image, not as an error. `unavailable` offers a retry once `retryAfter` has
  passed.
- **Loading:** the section loads only when it scrolls into view, and fetches nothing when the
  setting is off.

## Configuration

`server.image_attestations` (flag `-image-attestations`, env `CETACEAN_IMAGE_ATTESTATIONS`),
default `true`. It is separate from `server.image_check`, for two reasons:

- The update check runs on a schedule and only ever sends a HEAD for a tag.
- This feature downloads blobs when someone looks.

An operator may want one without the other. When it is off, nothing in the feature contacts a
registry. The SPA does not learn about the switch separately: it calls `/provenance`, which
answers `disabled` without any request to a registry. The meta endpoints stay unchanged. It goes
into `docs/configuration.mdx`, and the settings count in `CLAUDE.md` rises by one.

## Specifications

The requirements this design relies on go into `internal/spec/registry/`, each claimed by its test:

- **OCI image index and distribution:** fetching by digest, digest verification and the manifest
  media types.
- **in-toto Statement v1:** the subject and the predicate type.
- **SLSA provenance v0.2 and v1:** the fields read.
- **SPDX 2.3 JSON:** packages, `documentDescribes` and `DESCRIBES` relationships, and the license fields.

The BuildKit attestation layout is a Docker convention, not a specification. It is cited in
`docs/specs/` (here) and pinned by tests, not registered.

## Testing

- **Unit:** the imagecheck tests already have a fake registry. It gains:
  - an index with attestations for two platforms;
  - SLSA v0.2 and v1 predicates in both modes;
  - an SBOM over the cap;
  - a body with the wrong digest;
  - 401 and 429 responses.
- **Fixtures:** real predicates, trimmed, so that the parser is tested against what BuildKit
  emits, not against what we think it emits:
  - `nginx:latest`: v0.2 provenance, deb SBOM;
  - `docker/buildkit-syft-scanner:latest`: v1 `mode=max` provenance with GitHub context and
    embedded source; Go SBOM;
  - a local `mode=max` build from a git checkout, with several layers in the final stage, for the
    `vcs` fallback and the layer assignment;
  - a v0.2 `mode=max` build on GitHub Actions, for the GitHub fields.

  The layer assignment is tested against both `mode=max` fixtures together with their platform
  manifests.
- **e2e:** an image with attestations must be reachable anonymously over HTTPS. If the DinD
  environment cannot offer that, the e2e lane covers only the states `unpinned` and `disabled`,
  and `test/e2e/README.md` lists the gap.

## Documentation

- `docs/dashboard.md`: the Build section, the SBOM page and the Dockerfile page.
- `docs/api.md`, `api/openapi.yaml`, the JSON-LD context: the endpoints.
- `docs/mcp.md`: the resources.
- `docs/configuration.mdx`: the setting.
- `.claude/ARCHITECTURE.md`: the package, with two traps:
  - the platform names are normalized before matching;
  - content-addressed answers never expire, but the failure states do.
- `CHANGELOG.md`.

## Left to the builder

- How the digest-addressed reference is spelled, and whether `internal/attestations` holds its own
  `imagecheck.Client` or shares the checker's (sharing the token cache is better).
- The status codes for the SBOM problem types.
- Component structure and layout of the Build section, the SBOM page and the Dockerfile page,
  within the existing service-detail panel conventions.
- Which highlighter the Dockerfile page uses, if the frontend already ships one. Otherwise plain
  monospace is fine.
- Whether the SBOM table paginates on the server or on the client. With the 32 MiB cap, the
  client can hold any table that passes it.
