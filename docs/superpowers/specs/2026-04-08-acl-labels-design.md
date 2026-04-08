# ACL Labels Design

**Date:** 2026-04-08
**Issue:** [#11](https://github.com/Radiergummi/cetacean/issues/11)

## Summary

Label-based access control for Docker Swarm resources. Teams add `cetacean.acl.read` and `cetacean.acl.write` labels to their stack definitions to control who can access their resources, using the same audience syntax as the config policy. Labels are opt-in via `CETACEAN_ACL_LABELS=true`.

## Label Format

Two Docker labels on any labelable resource (service, config, secret, network, volume, node):

```yaml
services:
  myapp:
    deploy:
      labels:
        cetacean.acl.read: "group:dev,user:alice@example.com"
        cetacean.acl.write: "group:ops"
```

- **Key** is the permission level: `cetacean.acl.read` or `cetacean.acl.write`.
- **Value** is a comma-separated list of audience expressions using existing syntax: `user:pattern`, `group:pattern`, or `*`.
- `write` implies `read`, same as config policy.
- Whitespace around commas is trimmed.
- Invalid audience expressions are silently ignored with a warning log.

## Supported Resource Types

| Resource | Label source | Notes |
|---|---|---|
| Service | `service.Spec.Labels` | Deploy labels in compose |
| Config | `config.Spec.Labels` | |
| Secret | `secret.Spec.Labels` | |
| Network | `network.Labels` | |
| Volume | `volume.Labels` | |
| Node | `node.Spec.Labels` | Managed by cluster admins |
| Task | Inherited from parent service | Tasks never carry own ACL labels |
| Stack | N/A | Derived concept, no labels. Use config policy for stack-wide grants |

## Evaluation Logic

The evaluator's `Can(identity, permission, resource)` flow:

1. **Auth mode `none`** — allow, skip everything.
2. **Labels present** on the resource (any `cetacean.acl.*` label exists):
   - Identity matches a label audience expression → label permission is the effective permission. **Done.**
   - Identity does not match any label audience → labels suppress the implicit allow-all default. Continue to step 3.
3. **Config/provider grants:**
   - Explicit grant matches the resource for this identity → that permission applies. **Done.**
   - No grant matches → **deny** (regardless of whether a config policy file exists, because labels suppress the allow-all default for this resource).

When **no labels** exist on a resource, current behavior is unchanged:
- Config grant matches → applies.
- No grant, no policy → allow-all.
- No grant, policy active → deny.

`Filter()` follows the same logic since it calls `Can()` per item.

## Precedence Rules

- **Labels win over config** for identities that match a label audience. This lets teams narrow config-granted permissions on specific resources (e.g., restricting a control-plane stack to read-only for most groups even if config policy grants broad write).
- **Config fills gaps** for identities not mentioned in labels, but only via explicit grants — the implicit allow-all default is suppressed when labels are present.
- **Within a layer**, most permissive wins. Multiple matches are additive, no deny rules. This is consistent with config policy semantics.
- **Labels are strictly scoped** to the resource they are on. No stack fan-out — a label on one service in a stack does not affect other services in that stack.

### Example

Service has `cetacean.acl.write: "group:ops"` and `cetacean.acl.read: "group:*"`.

| Identity | Config grant | Label match | Effective |
|---|---|---|---|
| group:ops | `write` on `service:*` | `write` (label) | **write** (label wins) |
| group:dev | `write` on `service:*` | `read` (label) | **read** (label wins, narrows config) |
| user:bot | `write` on this service | No match | **write** (explicit config grant) |
| user:stranger | None | No match | **deny** (labels suppress allow-all, no config grant) |

## Task Inheritance

Tasks inherit ACL labels from their parent service. The evaluator already resolves tasks to their parent service via `ServiceOfTask()`. The same service name is used to look up labels via `LabelsOf("service", serviceName)`.

Container labels (`service.Spec.TaskTemplate.ContainerSpec.Labels`) are not checked — ACL labels belong in `deploy.labels`, not application-level container labels.

## Configuration

| Variable | TOML path | Default | Description |
|---|---|---|---|
| `CETACEAN_ACL_LABELS` | `acl.labels` | `false` | Enable label-based ACL evaluation |

When disabled, `cetacean.acl.*` labels on resources are ignored entirely. No performance cost in the evaluator hot path.

## Implementation: ResourceResolver Extension

Add one method to the `ResourceResolver` interface:

```go
LabelsOf(resourceType, resourceID string) map[string]string
```

The cache implementation looks up the resource by type and ID/name, returns its labels map (or nil if not found). Handles all six labelable types: service, config, secret, network, volume, node. Tasks and stacks return nil.

## Implementation: Evaluator Changes

In `Can()`, after collecting config/provider grants, add label evaluation:

1. Call `resolver.LabelsOf(type, name)` for the target resource. For tasks, resolve to parent service first.
2. Parse `cetacean.acl.read` and `cetacean.acl.write` values into audience lists.
3. Check if the identity matches any audience expression, tracking the highest matched permission.
4. If a label audience matched → return whether the matched permission satisfies the requested permission.
5. If no label audience matched but labels were present → suppress allow-all default, fall through to config/provider grants with explicit-only matching.
6. If no labels → existing behavior unchanged.

## Integration Panel

A new integration panel on the service detail page (and other applicable resource detail pages) for viewing and editing `cetacean.acl.*` labels. Follows the existing pattern established by Traefik, Shepherd, Swarm Cronjob, and Diun integrations.

The panel shows:
- Current read audience list
- Current write audience list
- Add/remove audience expressions inline

Editing requires operations level 2 (configuration), consistent with other integration label editors.

Detection: presence of any `cetacean.acl.*` label on the resource. When `CETACEAN_ACL_LABELS` is disabled, the panel still renders labels if present (they're valid Docker labels regardless) but shows a notice that label-based ACL evaluation is disabled.

## Observability

- **Debug log** when a label grant matches: permission, audience expression, resource.
- **Debug log** when labels are present but no audience matches: resource, identity subject.
- **Warning log** when a label value contains an unparseable audience expression: resource, label key, raw value.

`GET /auth/whoami` continues to reflect config/provider grants only. Per-resource label narrowing is not projected into the permissions map since it depends on which specific resource is being accessed.

## Performance

`LabelsOf()` is a read-locked map lookup in the existing in-memory cache — same cost as `StackOf()` which is already in the evaluator hot path. Label string parsing (comma split + trim) is trivial and not worth caching given invalidation complexity when labels change via Docker events.

## Out of Scope

- Deny rules or explicit exclusions.
- Stack-level label inheritance (labels on one resource covering sibling resources).
- Label-based ACL for plugins or swarm-level resources.
- Reflecting label grants in `GET /auth/whoami` permissions map.
