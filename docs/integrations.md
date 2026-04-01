# Integrations

Cetacean detects well-known Docker Swarm ecosystem tools configured via service labels and presents them as structured
panels on the service detail page. Instead of scrolling through raw `traefik.http.routers.myapp.rule=Host(...)` labels,
you see a formatted Traefik panel with routers, services, and middlewares laid out clearly.

Integration panels appear above the labels section on service detail pages. If no known labels are detected, no panels
are shown.

## Supported Tools

### Traefik

Detects all `traefik.*` labels. Parses `traefik.http.*` labels into structured routers, services, and middlewares.

**Detected fields:**

| Section | Fields |
|---|---|
| Routers | name, rule, entrypoints, TLS (cert resolver, domains, options), middlewares, target service, priority |
| Services | name, port, scheme |
| Middlewares | name, type, config key-value pairs |

The `traefik.enable` label maps to the panel's enabled/disabled indicator.

TCP and UDP labels (`traefik.tcp.*`, `traefik.udp.*`) are recognized but not parsed into structured views.

### Shepherd

Detects `shepherd.*` labels. [Shepherd](https://github.com/djmaze/shepherd) is a Docker Swarm service auto-updater.

| Field | Label | Description |
|---|---|---|
| Enable | `shepherd.enable` | Whether Shepherd watches this service |
| Auth config | `shepherd.auth.config` | Registry authentication configuration |

### Swarm Cronjob

Detects `swarm.cronjob.*` labels. [Swarm Cronjob](https://github.com/crazy-max/swarm-cronjob) creates cron-scheduled
jobs in Docker Swarm.

| Field | Label | Description |
|---|---|---|
| Enable | `swarm.cronjob.enable` | Whether cron scheduling is active |
| Schedule | `swarm.cronjob.schedule` | Cron expression |
| Skip running | `swarm.cronjob.skip-running` | Skip if a previous run is still in progress |
| Replicas | `swarm.cronjob.replicas` | Number of replicas per scheduled run |
| Registry auth | `swarm.cronjob.registry-auth` | Use registry authentication |
| Query registry | `swarm.cronjob.query-registry` | Query the registry before creating the job |

### Diun

Detects `diun.*` labels. [Diun](https://github.com/crazy-max/diun) monitors Docker images for updates and sends
notifications.

| Field | Label | Description |
|---|---|---|
| Enable | `diun.enable` | Whether Diun watches this service's image |
| Registry options | `diun.regopt` | Registry options name |
| Watch repo | `diun.watch_repo` | Watch all tags in the repository |
| Notify on | `diun.notify_on` | Events to notify on (semicolon-separated: `new`, `update`) |
| Sort tags | `diun.sort_tags` | Tag sort order (`default`, `reverse`, `semver`, `lexicographical`) |
| Max tags | `diun.max_tags` | Maximum number of tags to watch |
| Include tags | `diun.include_tags` | Regex pattern for tags to include |
| Exclude tags | `diun.exclude_tags` | Regex pattern for tags to exclude |
| Hub link | `diun.hub_link` | Custom registry hub link |
| Platform | `diun.platform` | Platform to watch (e.g. `linux/amd64`) |
| Metadata | `diun.metadata.*` | Arbitrary key-value metadata |

## Editing

Integration settings can be edited inline on the service detail page. Click Edit on any integration panel to switch to
form mode. Each field maps directly to its underlying Docker service label -- saving writes the labels back via the
standard labels endpoint.

Editing requires [operations level](configuration.md) 2 (configuration) or higher.

A structured/raw toggle lets you switch between the form editor and a raw key-value label editor. The toggle is locked
for the duration of an edit session.

For Traefik, editing is limited to existing routers, services, and middlewares. Adding or removing objects requires the
raw labels editor. Unrecognized Traefik labels (TCP, UDP) are preserved through edits.

## API

Integration data is returned in the service detail response as an `integrations` array:

```json
{
  "@type": "Service",
  "service": { ... },
  "integrations": [
    {
      "name": "traefik",
      "enabled": true,
      "routers": [{ "name": "myapp", "rule": "Host(`example.com`)", ... }],
      "services": [{ "name": "myapp", "port": 8080 }],
      "middlewares": [{ "name": "auth", "type": "basicauth", "config": { ... } }]
    }
  ]
}
```

The field is omitted when no integrations are detected. Raw labels in the service spec are unchanged -- the frontend
filters consumed label keys from the labels editor when an integration panel is present.

Integration edits go through the existing `PATCH /services/{id}/labels` endpoint using JSON Patch.
