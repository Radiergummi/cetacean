# Attestation fixtures

Real BuildKit attestations, trimmed, so the parsers are tested against what
BuildKit emits rather than what we think it emits. Captured 2026-10-27.

| File | From |
|---|---|
| `nginx-provenance.json`, `nginx-sbom.json` | `nginx:latest`, `linux/amd64`. SLSA v0.2 provenance with embedded source; a Debian SBOM |
| `syft-provenance.json`, `syft-sbom.json` | `docker/buildkit-syft-scanner:latest`, `linux/amd64`. SLSA v1 `mode=max` provenance from GitHub Actions; a Go SBOM |
| `probe/` | An OCI layout built locally from `probe.Dockerfile`, with `--provenance=mode=max` from a git checkout whose `origin` is `https://github.com/example/attest-probe.git`. Its layer blobs are left out; only the descriptors are needed |
| `hub-digests.json` | The index digests the two Docker Hub fixtures were pulled from |

Each `*.json` is an in-toto statement exactly as its blob holds it, minus what
`scripts/attestations-fixtures/fixtures.py` trims:

- **nginx provenance:** `buildConfig.llbDefinition` is cut to its first three steps.
- **syft provenance:** `internalParameters.github_event_payload` is dropped.
- **SBOMs:** only the described root package and a handful of others are kept, with the
  relationships between them. nginx keeps `abseil`, `acl`, `aom`, one deb that declares no
  license, and its one Maven package. syft keeps six Go modules.

Regenerate:

```bash
python3 -I scripts/attestations-fixtures/fixtures.py hub internal/attestations/testdata

# in a git checkout holding probe.Dockerfile as Dockerfile, and a hello.txt
docker buildx build --builder <a docker-container builder> --platform linux/arm64 \
  --provenance=mode=max --sbom=false --output type=oci,dest=<dir>,tar=false .
python3 -I scripts/attestations-fixtures/fixtures.py probe <dir> internal/attestations/testdata
```

Known gap: no fixture has SLSA v0.2 `mode=max` provenance from GitHub Actions,
so where v0.2 records the `github_*` parameters is unverified and not read.
