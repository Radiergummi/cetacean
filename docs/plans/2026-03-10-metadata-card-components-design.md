# Reusable Metadata Card Components

## Problem

Detail pages use raw `InfoCard` with ad-hoc value formatting for resource links, images, timestamps, and IDs. This leads to inconsistencies (e.g., TaskDetail shows image without registry link, ServiceDetail shows it with one) and repetitive code.

## Design

New components in `frontend/src/components/data/`. Each wraps `InfoCard` internally and replaces it at call sites.

### `ResourceId`

Displays a resource identifier, optionally truncated, in monospace.

```tsx
interface Props {
  label: string;
  id: string;
  truncate?: number; // max chars, omit for full ID
}
```

Returns `null` if `id` is empty. Renders truncated value with full ID as `title` attribute.

Usage: Config/Secret/Network ID cards, Container ID in TaskDetail.

### `ResourceLink`

Links to another Cetacean resource page. Shows a human-friendly name, routes by ID.

```tsx
interface Props {
  label: string;
  name: string;    // display label (e.g., service name, hostname)
  to: string;      // route path (e.g., `/services/${id}`)
}
```

Returns `null` if `name` is empty. Caller constructs the route — no resource-type routing logic in the component.

Usage: Service/Node/Stack links in TaskDetail, ServiceDetail, ConfigDetail, SecretDetail, NetworkDetail, VolumeDetail.

### `ContainerImage`

Image name with registry link and registry favicon.

```tsx
interface Props {
  image: string;         // full image reference (with digest/tag)
  label?: string;        // defaults to "Image"
}
```

Strips `@sha256:` digest for display. Uses `imageRegistryUrl()` for href. Shows a small favicon from the registry domain (Docker Hub, ghcr.io, quay.io, gcr.io) inline before the image name. Returns `null` if `image` is empty.

Usage: ServiceDetail, TaskDetail, StackDetail service tables.

### `Timestamp`

Wraps InfoCard with TimeAgo or absolute date display.

```tsx
interface Props {
  label: string;
  date?: string;
  relative?: boolean; // default true
}
```

When `relative=true` (default), renders `<TimeAgo>`. When `false`, renders `toLocaleString()`. Returns `null` if `date` is missing.

Usage: Created/Updated cards in Config, Secret, Network, Volume, Service detail pages. Timestamp card in TaskDetail.

## Null-safety convention

All components return `null` when their primary value is missing. This eliminates conditional wrappers at call sites — instead of `{value && <InfoCard .../>}`, callers write `<Timestamp label="Created" date={resource.CreatedAt} />` unconditionally.

## Scope

- Create 4 components in `frontend/src/components/data/`
- Create `index.ts` barrel export
- Replace matching `InfoCard` usages across all 7 detail pages (Service, Task, Node, Config, Secret, Network, Volume)
- StackDetail service table: use `ContainerImage` inline (not as InfoCard, just the display/link portion) — or skip if it doesn't fit the card pattern
