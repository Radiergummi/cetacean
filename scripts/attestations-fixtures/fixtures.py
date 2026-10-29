"""Regenerate internal/attestations/testdata from real images.

  python3 -I scripts/attestations-fixtures/fixtures.py hub <out-dir>
  python3 -I scripts/attestations-fixtures/fixtures.py probe <oci-layout> <out-dir>

`hub` pulls the linux/amd64 attestations of nginx:latest and
docker/buildkit-syft-scanner:latest anonymously from Docker Hub and trims
them; `probe` copies the manifests and attestation blobs (not the layers) of
an OCI layout written by `buildx build --output type=oci,tar=false`.
"""

import hashlib
import json
import os
import shutil
import sys
import urllib.request

INDEX_TYPES = (
    "application/vnd.oci.image.index.v1+json",
    "application/vnd.docker.distribution.manifest.list.v2+json",
)
ACCEPT = ", ".join(INDEX_TYPES + (
    "application/vnd.oci.image.manifest.v1+json",
    "application/vnd.docker.distribution.manifest.v2+json",
))
SPDX = "https://spdx.dev/Document"


def get(url, token=None, accept=None):
    req = urllib.request.Request(url)
    if token:
        req.add_header("Authorization", "Bearer " + token)
    if accept:
        req.add_header("Accept", accept)
    with urllib.request.urlopen(req, timeout=60) as resp:
        return resp.read(), resp.headers


def pull(repository, tag, platform=("linux", "amd64")):
    token = json.loads(get(
        "https://auth.docker.io/token?service=registry.docker.io"
        f"&scope=repository:{repository}:pull")[0])["token"]
    base = f"https://registry-1.docker.io/v2/{repository}"
    index_body, _ = get(f"{base}/manifests/{tag}", token, ACCEPT)
    index = json.loads(index_body)
    image = next(m for m in index["manifests"]
                 if (m["platform"]["os"], m["platform"]["architecture"]) == platform)
    attestation = next(
        m for m in index["manifests"]
        if m.get("annotations", {}).get("vnd.docker.reference.digest") == image["digest"])
    manifest = json.loads(get(f"{base}/manifests/{attestation['digest']}", token, ACCEPT)[0])
    statements = {}
    for layer in manifest["layers"]:
        kind = layer["annotations"]["in-toto.io/predicate-type"]
        body, _ = get(f"{base}/blobs/{layer['digest']}", token)
        assert "sha256:" + hashlib.sha256(body).hexdigest() == layer["digest"]
        statements[kind] = json.loads(body)
    return "sha256:" + hashlib.sha256(index_body).hexdigest(), statements


def trim_sbom(statement, keep):
    doc = statement["predicate"]
    described = {r["relatedSpdxElement"] for r in doc.get("relationships", [])
                 if r["spdxElementId"] == "SPDXRef-DOCUMENT"
                 and r["relationshipType"] == "DESCRIBES"}
    packages = [p for p in doc["packages"] if p["SPDXID"] in described or keep(p)]
    ids = {p["SPDXID"] for p in packages} | {"SPDXRef-DOCUMENT"}
    doc["packages"] = packages
    doc["relationships"] = [r for r in doc.get("relationships", [])
                            if r["spdxElementId"] in ids and r["relatedSpdxElement"] in ids]
    doc["files"] = []
    return statement


def write(out, name, value):
    with open(os.path.join(out, name), "w") as f:
        json.dump(value, f, indent=1, sort_keys=True)
        f.write("\n")


def hub(out):
    digests = {}

    digests["nginx:latest"], nginx = pull("library/nginx", "latest")
    provenance = next(v for k, v in nginx.items() if "slsa.dev/provenance" in k)
    provenance["predicate"]["buildConfig"]["llbDefinition"] = \
        provenance["predicate"]["buildConfig"]["llbDefinition"][:3]
    write(out, "nginx-provenance.json", provenance)
    wanted = {"acl", "aom", "abseil"}
    state = {"maven": 0, "unasserted": 0}

    def keep_nginx(p):
        purl = next((r["referenceLocator"] for r in p.get("externalRefs", [])
                     if r["referenceType"] == "purl"), "")
        if p["name"] in wanted:
            return True
        if purl.startswith("pkg:maven/") and not state["maven"]:
            state["maven"] += 1
            return True
        if purl.startswith("pkg:deb/") and p.get("licenseDeclared") in (None, "NOASSERTION") \
                and not state["unasserted"]:
            state["unasserted"] += 1
            return True
        return False

    write(out, "nginx-sbom.json", trim_sbom(nginx[SPDX], keep_nginx))

    digests["docker/buildkit-syft-scanner:latest"], syft = pull(
        "docker/buildkit-syft-scanner", "latest")
    provenance = next(v for k, v in syft.items() if "slsa.dev/provenance" in k)
    provenance["predicate"]["buildDefinition"]["internalParameters"].pop(
        "github_event_payload", None)
    write(out, "syft-provenance.json", provenance)
    count = {"n": 0}

    def keep_syft(p):
        count["n"] += 1
        return count["n"] <= 6

    write(out, "syft-sbom.json", trim_sbom(syft[SPDX], keep_syft))
    write(out, "hub-digests.json", digests)


def probe(layout, out):
    target = os.path.join(out, "probe")
    shutil.rmtree(target, ignore_errors=True)
    os.makedirs(os.path.join(target, "blobs", "sha256"))
    for name in ("index.json", "oci-layout"):
        shutil.copy(os.path.join(layout, name), target)

    def blob(digest):
        return os.path.join(layout, "blobs", *digest.split(":"))

    def keep(digest):
        shutil.copy(blob(digest), os.path.join(target, "blobs", *digest.split(":")))

    with open(os.path.join(layout, "index.json")) as f:
        top = json.load(f)["manifests"][0]["digest"]
    keep(top)
    with open(blob(top)) as f:
        index = json.load(f)
    for entry in index["manifests"]:
        keep(entry["digest"])
        with open(blob(entry["digest"])) as f:
            manifest = json.load(f)
        keep(manifest["config"]["digest"])
        if entry["platform"]["os"] == "unknown":
            for layer in manifest["layers"]:
                keep(layer["digest"])


if __name__ == "__main__":
    if sys.argv[1] == "hub":
        hub(sys.argv[2])
    else:
        probe(sys.argv[2], sys.argv[3])
