# Backlog

Working scratch, deliberately untracked. Everything still open after 0.13.0,
consolidated from the plan and review docs before they left `docs/specs/`, from
the open issues, and from the MCP evaluation set. Delete it when it is empty or
when the items have become issues.

Verified against `main` at `2343d3f0`: CI green, `[Unreleased]` empty, one TODO
in the source tree, all 45 MCP review findings marked fixed, issues #69-74 all
closed. Docker tag aliases (`v0`, `v0.13`, `latest`) and release attestation are
already in `release.yml` — both were on the loose-end list and are done.

## In flight

- **PR #197 — internal docs consolidation.** Plans and point-in-time reviews out
  of `docs/specs/`, `docs/specs/README.md` recording the split. All 11 checks
  green; ready to merge.
- **PR #198 — label-based ACL,** superseding #16 and closing #11. Ported onto
  current `main` with two defects fixed on the way: the allow-all suppression was
  global rather than per resource (one boolean turned an unpolicied cluster dark),
  and `Filter` was quadratic against the real cache — 41ms to filter a page of
  2000 services, now 1.2ms. #16 is closed with both recorded.
- **Validate 0.13.0 live.** The evaluation set's coverage markers were re-derived
  by reading the build, not by asking a cluster, so the release's headline claim
  is unverified end to end. Rolling out to prod to run it. `docs/test_protocol.md`
  now has a "Label-Based ACL" section that also wants a real cluster with a
  non-`none` auth provider.

## Housekeeping

- Delete `feature/mcp-server` — 5 months old, 113 behind, 50 conflicts, entirely
  superseded by the MCP server that shipped in 0.13.0.
- Delete `fix/release-build-widgets` — fully in `main` via the #194/#195 squashes;
  `main` is now strictly ahead of it. Remote is already gone.
- The worktree at `../cetacean-acl-labels` now holds a closed branch. It is the
  only remaining copy of #16's original 23 commits outside the reflog, so keep it
  until #198 lands, then remove both it and `feature/acl-labels`.
- The `- [ ]` checkboxes in the moved plan files were never maintained — every
  plan showed 0 of N checked including the ones that fully shipped. If plans come
  back, either maintain them or drop the checkbox syntax.

## Stagnant work — needs a decision, not a task

- **`feature/alertmanager-integration`** — the largest unlanded feature in the
  repo and never opened as a PR. 35 commits, 228 behind, 55 files: an
  `internal/alertmanager` package (client, discovery, webhook), alert and silence
  API handlers, swarm state gauges, Prometheus rules wired to Cetacean's own
  metrics, and a frontend alert badge. 18 conflicts, concentrated where it hurts —
  `docs/configuration.md` no longer exists (it is `configuration.mdx`), plus
  `App.tsx`, `api/types.ts`, `NodeDetail.tsx`.
  Per-commit rebase is not the move at this distance: squash to one diff and
  re-apply, or re-implement with the branch as reference. Sits in
  `.worktrees/alertmanager`.

## Carried forward from the removed reviews

Two items that were real and are not in any issue:

- **`SegmentPrefixMatch` allocates a memo map per call on a hot path** —
  `internal/cluster/search.go:457`, the one TODO in the tree. Was M-39, closed
  with the marker rather than the fix, pending a benchmark to show it matters.
  Benchmark first; the fix is only worth it if the benchmark says so.
- **`BacklogFilter` clock-drift suppression** — the one residual the SSE review
  bounded rather than eliminated. A task's backlog ends only when one of *that
  task's* lines clears the cursor, so on a multi-node swarm a replica whose node
  clock trails the one that stamped the cursor stays silent for the drift
  duration after a resume, behind a healthy "Live" badge. It self-heals and no
  other replica is affected. Bounding the backlog by line count or elapsed time,
  rather than purely by "a line cleared the cursor", would close it.

## Found while porting #198

- **`internal/acl/bench_test.go` benchmarks the stub, not the cache.** Every ACL
  benchmark drives a map-backed `stubResolver`, which is why a 41ms quadratic
  `Filter` sat in the branch for five months without showing up. The
  `TestFilter_ReadsLabelsOncePerType` guard covers the specific regression, but a
  benchmark against the real `cache.Cache` would catch the next one of these by
  measurement rather than by reading. Cheap to add: the throwaway one used to get
  the numbers above lived in `internal/cache` and imported `acl`, which is
  acyclic.

## Open issues

- **#171** jitter `Retry-After` server-side so clients we do not control also
  de-synchronize. Small, well-scoped, filed 2026-09-07.
- **#89** Windows nanoserver container image.
- **#5** show image vulnerabilities on the service detail page via Trivy.
- **#6** interactive container shell via ghostty-web — blocked: needs the
  per-node agent architecture first, so it is not pickable as written.

## Agent surface — from the evaluation set

`docs/specs/2026-09-07-mcp-evaluation-set.md` is the source and stays
authoritative; this is only the shape of what it leaves open. 17 of 73 asks are
still short, and the split is the finding: only four want a genuinely new *read*.
The rest is composition and self-description.

Highest leverage, because it needs **no new tools** — three prompts, ranked by
how often the asks recur and what they cost unaided:

1. **Secret rotation** (asks 53, 60, 38) — four steps, a destructive last one, an
   ordering that matters. The eval set calls it the strongest candidate by some
   distance, and 0.13.0 just shipped every underlying call (`create_secret`,
   `update_service_secrets`, `remove_secret`).
2. **Incident triage** (2, 3, 7, 24, 25) — the on-call entry point, three or four
   separate calls every time.
3. **Deploy and verify** (15-18, 20) — push, wait, confirm, roll back if it did
   not take.

Then, in rough order of value:

- **Bulk / fan-out argument** (61 restart a stack, 62 scale a stack). One call
  per member service is the entire cost.
- **Self-description** (65 what am I allowed to change, 66 why was that refused).
  An agent currently discovers its limits by being refused, and cannot tell a
  permission failure from an impossibility.
- **Aggregates over existing reads** (44 orphaned secrets, 45 image inventory).
  Both are answerable one resource at a time, which is the problem.
- **New reads** (21 spec diff, 34 volume fill, 41 published ports, 73 socket
  mounts and privileged services).
- **Watch, do not fix** (12 app or infrastructure, 32 will it fit). Judgement, not
  lookup — if a prompt makes either reliable, that is the answer.
- **Out of scope** (22 stack deploy, a compose-file operation; 27 live tail, which
  the transport cannot do — repeated cursor reads are the closest thing and are
  what the logs widget already does).
