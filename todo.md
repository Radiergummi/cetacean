# TODOs for Docker Swarm UI project

## Operational gaps
- ~~RBAC / authorization — auth is in, but everyone sees everything. Role-based access (read-only vs. admin, per-stack scoping) would be the natural follow-up.~~
- ~~Write operations — service scaling, rolling restarts, force-removing tasks, env/labels/resources/placement/ports/policy/log-driver editors all implemented with operations level gating.~~
- ~~Alerting / notifications — threshold-based alerts on resource usage, unhealthy services, or task failure spikes (webhook, email, Slack).~~

## Visibility improvements
- Container exec / shell — interactive terminal into running containers (WebSocket-based). -> problematic without sidecar
- ~~Diff / audit trail — show what changed in a service spec between deployments (config diff view).~~
- Dependency graph — visualize service dependencies beyond network topology (e.g., which services share
  configs/secrets/volumes).
- Image vulnerability scanning — pull CVE data for running images (Trivy integration or similar).

## Polish / DX
- Saved filters / bookmarks — persist complex filter expressions as named views.
- Dashboard customization — let users pin specific services/nodes/metrics to a home view.
- ~~Mobile / responsive layout — if it's used as an on-call tool, phone-friendly views matter.~~
