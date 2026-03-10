# Stack Summary Endpoint & StackList Redesign

## Goal

Replace the bland StackList page with a DevOps-oriented overview that shows cluster health at a glance: task states, resource usage, and deploy status per stack — all from a single API call.

## API: `GET /api/stacks/summary`

Returns `[]StackSummary`:

```json
{
  "name": "myapp",
  "serviceCount": 4,
  "configCount": 2,
  "secretCount": 1,
  "networkCount": 2,
  "volumeCount": 1,
  "desiredTasks": 8,
  "tasksByState": { "running": 7, "failed": 1 },
  "updatingServices": 0,
  "memoryUsageBytes": 1073741824,
  "memoryLimitBytes": 2147483648,
  "cpuUsagePercent": 45.2,
  "cpuLimitCores": 4.0
}
```

### Backend implementation

1. Iterate cached stacks. For each stack, aggregate from cached data:
   - `serviceCount`, `configCount`, `secretCount`, `networkCount`, `volumeCount` (already available)
   - `desiredTasks`: sum of `Spec.Mode.Replicated.Replicas` across services (global services count nodes)
   - `tasksByState`: count tasks by `Status.State` for all services in the stack
   - `updatingServices`: count services where `UpdateStatus.State == "updating"`
   - `memoryLimitBytes`: sum of `Spec.TaskTemplate.Resources.Limits.MemoryBytes` across services (multiplied by replica count)
   - `cpuLimitCores`: sum of `Spec.TaskTemplate.Resources.Limits.NanoCPUs / 1e9` across services (multiplied by replica count)

2. Query Prometheus (two queries total, not per-stack):
   - `sum by (container_label_com_docker_stack_namespace)(container_memory_usage_bytes)`
   - `sum by (container_label_com_docker_stack_namespace)(rate(container_cpu_usage_seconds_total[5m])) * 100`

3. Join Prometheus results into the per-stack structs by matching the `container_label_com_docker_stack_namespace` label to stack name.

4. If Prometheus is unreachable or returns errors, return the response with zero usage values. The endpoint must not fail because of Prometheus.

### Handler location

New handler in `internal/api/handlers.go`, registered in `router.go` as `GET /api/stacks/summary`.

Prometheus querying should use the existing Prometheus URL from config, but as a direct HTTP client call (not via the proxy handler). A small helper to execute an instant PromQL query and parse the vector result is sufficient.

## Frontend: StackList redesign

Replace the current table/card view with a grid of stack health cards. Each card shows:

- **Stack name** (links to stack detail)
- **Health indicator**: green if all desired tasks are running, yellow if some are pending/starting, red if any are failed
- **Task bar**: compact stacked bar showing running/failed/other proportionally
- **Resource usage**: memory and CPU bars showing usage vs limit (e.g., "1.2 / 2.0 GB")
- **Deploy badge**: visible only when `updatingServices > 0`
- **Resource counts**: small footer showing service/config/secret/network/volume counts

### Layout

- Grid of cards, responsive (1 col mobile, 2 col tablet, 3 col desktop)
- Search filter retained
- No table view needed — the card view is the primary and only view for this page

## Constraints

- Single API call for the entire page
- Prometheus failure must not break the page
- No raw PromQL exposed to the frontend
