# Compose export: design

## Problem

Cetacean can describe a service completely — image, replicas, placement,
resources, mounts, secrets, health check, update policy — and offers no way to
get that description back out in the form the ecosystem writes it in. The
dashboard's stated goal is to be a complete replacement for the Docker CLI when
it comes to *understanding* a Swarm cluster, and understanding a stack and then
being unable to reproduce it is where that goal stops short.

The gap has three shapes in practice. A stack deployed by hand months ago has no
compose file anywhere; reconstructing one means reading `docker service inspect`
for every member and translating by hand. A stack that does have a file has
drifted from it, and there is nothing to diff against. A stack that needs to
exist on a second cluster has to be rebuilt from the same manual translation.

None of this needs new data. `cache.StackDetail` already assembles the five
things a compose file names — services, configs, secrets, networks, volumes —
on the path the JSON detail endpoint already serves, behind the same ACL gate
(a stack grant covers its members, so the check is one `read` on
`stack:<name>`).
What is missing is a projection.

## Decision

A single YAML document, rendered from the live spec, that **redeploys to the
same running state on the same cluster**.

That promise is narrower than two adjacent ones, deliberately:

- It is not byte-identical to whatever file originally created the stack.
  Swarm does not keep that file, and reconstructing it would mean guessing at
  which of the values it holds were written and which were defaulted.
- It is not portable to another cluster on its own. Secret values are never
  exported — Cetacean's hard rule — and configs are referenced rather than
  inlined, so the target cluster needs them created first. The file says so in
  a comment rather than pretending otherwise.

Both are recorded here because the second one is the request people will make
next, and the answer is a bundle format, not a change to this document.

Scope is the stack and the single service. A service exports as a one-service
compose file: the same projection with a one-element `services` map, which is
what makes the service detail page — where people actually spend their time —
able to offer this at all.

## The package

One new package, `internal/compose`, holding the document types, the projection
from Docker SDK types, and the renderer:

```go
func FromStack(d cache.StackDetail) (File, []string)
func FromService(svc swarm.Service) (File, []string)
func Render(f File, warnings []string) ([]byte, error)
```

The `[]string` is the warnings block — see *Warnings*, below.

`FromService` needs no cache lookups because a single service owns nothing. It
references networks, volumes, configs and secrets; it does not create them, so
every one of them is `external: true` and the names on the spec itself are
enough. The owned/external question below is a stack question only.

It is deliberately not split the way `api/jgf` is split from `api/dot` and
`api/graphml`. That split exists because three renderers share one graph; here
there is one output format, and two packages for one format is structure with
nothing behind it. If a second format ever appears — the bundle, most likely —
the split is mechanical at that point and not before.

It is also not in `internal/cluster`, though both transports call it, which is
that package's stated bar. `cluster` holds the rules the transports must not
disagree about: what a task's parent is named, which of a node's tasks are live.
A serialization format is not such a rule — REST and MCP call the same function
here for the same reason they both call `atom.Render`, which lives in
`api/atom`. The distinction matters because `cluster`'s own note warns that
putting a one-sided projection there invites the next reader to assume a sharing
that does not exist; putting a genuinely shared *serializer* there would blur
the same line from the other direction.

`internal/compose` imports `internal/cache` and the Docker SDK types. `cache`
imports neither, so this is acyclic.

## Projection rules

The mapping is mostly mechanical. What follows is the part that is not — the
cases where emitting the field verbatim produces a file that looks correct and
deploys wrong.

### Names

Swarm holds `web_api`; compose says `api` under a stack named `web`. Every
service, network, volume, config and secret key has its `<stack>_` prefix
stripped, because `docker stack deploy` adds it back. Without this the file
redeploys as `web_web_api`.

A resource whose Swarm name does not follow the convention — one created outside
the stack and adopted into it — keeps its full name as the key and carries an
explicit `name:` field, so the reference resolves to the resource that actually
exists.

### Labels

`com.docker.stack.namespace` comes off every resource, for the same reason: the
deploy adds it. When stripping it leaves the map empty the key is omitted
entirely rather than emitted as `labels: {}`.

Container labels (`ContainerSpec.Labels`) and service labels
(`ServiceSpec.Annotations.Labels`) map to `labels` and `deploy.labels`
respectively. Swarm makes this distinction and hand-written files routinely get
it wrong, so keeping them apart is part of what the export is worth.

### Owned versus external

A stack's services routinely attach to networks the stack did not create — a
shared `monitoring` overlay is the common case. A network or volume carrying the
stack's namespace label is declared with its driver, options and attachability;
everything else is `external: true`. (In a single-service export nothing is
owned, so this reduces to declaring everything external.)

Getting this backwards is the single most likely way to produce a file that
reads correctly and fails to deploy, in both directions: declaring an external
network makes the deploy try to create one that exists, and marking an owned
network external makes it fail to find one that does not.

Configs and secrets are always `external: true`. For configs this is a choice
rather than a necessity — their content *is* available, base64-encoded — but
inlining it via compose's `content:` field would ask Swarm to create a config
on redeploy rather than reuse the existing one, which breaks the promise in the
Decision above. For secrets it is not a choice: the value is never exported.

### Images

The digest is kept. Swarm rewrites `nginx:1.27` to `nginx:1.27@sha256:…` at
deploy time, and the pinned form is what redeploys to the same running state.
`cluster.StripImageDigest` exists if the other behaviour is ever wanted.

### Dropped entirely

Runtime state that a spec does not carry and a deploy would reject or ignore:
`ID`, `Meta.Version`, `CreatedAt`, `UpdatedAt`, `Endpoint.VirtualIPs`,
`UpdateStatus`, `PreviousSpec`, `ServiceStatus`, `JobStatus`,
`TaskSpec.ForceUpdate`, and generated network and volume IDs.

### Fields needing conversion, not copying

These are the ones where a verbatim copy is wrong in a way that is easy to miss:

| Swarm | Compose | Conversion |
|---|---|---|
| `ContainerSpec.Hosts` | `extra_hosts` | Swarm writes `"1.2.3.4 host alias"` (hosts(5) order); compose writes `"host:1.2.3.4"`. Fields reverse, and each alias becomes its own entry. |
| `ContainerSpec.StopGracePeriod` | `stop_grace_period` | `time.Duration` nanoseconds to `10s`. |
| `ContainerSpec.Healthcheck` | `healthcheck` | `Interval`, `Timeout`, `StartPeriod`, `StartInterval` are all nanosecond counts and all become duration strings. Same conversion the healthcheck editor already applies. |
| `Resources.Limits.NanoCPUs` | `deploy.resources.limits.cpus` | `500000000` to the string `"0.50"`. |
| `Resources.Limits.MemoryBytes` | `deploy.resources.limits.memory` | Byte-suffixed string (`512M`) when it divides evenly, raw byte count otherwise. |
| `ContainerSpec.Env` | `environment` | `[]string` of `K=V` to a map. A bare `K` with no `=` — meaning "inherit from the daemon" — maps to a null value, not an empty string. |
| `ContainerSpec.Mounts` | `volumes` | Long syntax, with the source name shortened per *Names* for stack-owned volumes. |
| `EndpointSpec.Ports` | `ports` | Long syntax. `PublishMode` maps to `mode: ingress`/`host`. |
| `EndpointSpec.Mode` | `deploy.endpoint_mode` | Direct, but it lives under `deploy`, not beside `ports`. |
| `TaskSpec.LogDriver` | `logging` | Direct. |
| `ServiceMode.ReplicatedJob` / `GlobalJob` | `deploy.mode` | `replicated-job` / `global-job`. |

`Placement.Platforms` holds a list; compose's `platform` is a single value. One
entry maps; more than one emits the first and a warning.

`Privileges` splits across `credential_spec`, `security_opt` and
`cap_add`/`cap_drop`. A custom seccomp profile (`Privileges.Seccomp.Profile`,
raw JSON bytes) has no compose representation at all and produces a warning.

A service whose `TaskSpec.Runtime` is `plugin` or `attachment` has no
`ContainerSpec` and is not a compose service. It is omitted from the document
with a warning naming it.

## Warnings

Everything the projection could not carry is collected as a `[]string` and
rendered as a leading `#` comment block. A fixed header always states that
secrets and configs are external and what that means for redeploying elsewhere;
per-resource lines are appended for the cases above.

This is a slice of strings and a comment block, not a Note type with severities
and categories. The honesty is the point; the framework would be the speculative
part.

## Surfaces

### REST

`ContentTypeYAML` joins the enum in `internal/api/negotiate.go`, recognising the
`.yaml` and `.yml` extension suffixes and the `application/yaml` and
`text/yaml` media types.

`GET /stacks/{name}` and `GET /services/{id}` move from `contentNegotiated` to
the inline switch that `GET /topology` already uses for its three renderings.
Two call sites change; the twenty other `contentNegotiated` callers do not.

One consequence has to be handled rather than inherited: once `negotiate`
accepts `.yaml` globally, `GET /nodes/{id}.yaml` falls into `contentNegotiated`'s
`default:` branch and serves JSON under a YAML extension. Both dispatchers gain
an explicit `case ContentTypeYAML` returning `API003`, so an endpoint that does
not offer YAML says so instead of lying about its content type.

Caching is free: `writeRawWithETag` already takes bytes.

### MCP

A templated resource, `cetacean://stacks/{name}/compose` and
`cetacean://services/{id}/compose`, MIME `application/yaml`, resolved through
`lookupResource` so the ACL check and the name resolution are the existing ones.

A resource rather than a tool: this is a read, it costs nothing in `tools/list`,
and `services/logs` is the existing precedent for a `/`-suffixed template. The
`describe` tool's rule that an id may not contain `/` continues to hold and is
what keeps the two templates unambiguous.

### Dashboard

A collapsible Compose section on the stack and service detail pages, rendered in
the existing `CodeBlock`, with copy-to-clipboard and download. `api/client.ts`
gains a text-returning fetch — every existing method sets
`Accept: application/json`, so this is the first one that does not.

## Error handling and ACL

The YAML handler assembles `StackDetail` through the same filtered path the JSON
handler uses. Not a similar path — the same one.

This is the failure mode the topology builders already demonstrated: two
projections of one relationship derived independently, disagreeing about which
resources belong, and nobody noticing until a `dnsrr` service showed no network
attachments. Here the equivalent bug leaks a service the caller has no grant
for, which is worse than wrong output. A test asserts that a restricted identity
exporting a stack gets the same member set the JSON detail endpoint gives it.

A missing stack or service takes the existing 404 path. A spec containing
something unrepresentable never fails the document — it emits what it can and
appends a warning, because a partial file with an honest comment is more useful
than a 500. A YAML marshalling failure is a genuine 500 via `writeProblem`.

## Testing

Table tests in `internal/compose`, one per rule above — label stripping, name
shortening, the owned/external split in both directions, each conversion in the
table, and each warning case.

Golden files for whole documents: a fixture stack in, a committed `.yaml` out.
Goldens are the right shape here because the failure mode being defended against
is a field quietly changing shape, which a diff shows and an assertion on one
field does not.

A `docs/test_protocol.md` section covers what the unit tests cannot: export the
demo stack from a real cluster, `docker stack deploy` the result, and diff the
re-derived service specs against the originals.

The stronger option — parsing the emitted YAML with `compose-spec/compose-go`
in CI and asserting the derived spec matches the input modulo the documented
elisions — is deferred rather than rejected. It genuinely defends the promise in
the Decision, and it costs a large new test-only dependency. Revisit if the
goldens prove too weak in practice; the manual protocol step is the interim
answer.

## Out of scope

- **Bundle export.** A tar or zip carrying `compose.yaml` plus a `configs/`
  directory of real files is the artifact that makes cross-cluster migration
  work. It is a different response shape — not content-negotiable text, no ETag
  story, an awkward MCP result — and it should be its own design if the demand
  appears.
- **Import.** Nothing here reads a compose file. Deploying a stack is a
  compose-file operation against the Docker CLI's own stack machinery, and
  Cetacean has no create path for services today. Whether that boundary is
  deliberate is a separate open question; this design neither assumes nor
  changes the answer.
- **Diffing against a file on disk.** Useful, and a separate feature: it needs
  a parser, an upload path, and a rendering of the difference.
