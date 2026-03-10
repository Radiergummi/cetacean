# Metadata Card Components Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create reusable metadata card components (`ResourceId`, `ResourceLink`, `ContainerImage`, `Timestamp`) and replace ad-hoc `InfoCard` usages across all detail pages.

**Architecture:** Four components in `frontend/src/components/data/`, each wrapping `InfoCard` internally. Barrel-exported via `index.ts`. All return `null` when their primary value is missing, eliminating conditional wrappers at call sites.

**Tech Stack:** React, TypeScript, existing `InfoCard`, `TimeAgo`, `imageRegistryUrl()` helpers.

**Design doc:** `docs/plans/2026-03-10-metadata-card-components-design.md`

---

### Task 1: Create `ResourceId` component

**Files:**
- Create: `frontend/src/components/data/ResourceId.tsx`

**Step 1: Create the component**

```tsx
import InfoCard from "../InfoCard";

export default function ResourceId({
  label,
  id,
  truncate,
}: {
  label: string;
  id?: string;
  truncate?: number;
}) {
  if (!id) return null;
  const display = truncate ? id.slice(0, truncate) : id;
  return <InfoCard label={label} value={display} />;
}
```

Note: When `truncate` is set, `InfoCard` will set `title={display}` (since value is a string), showing the truncated value on hover. For full ID hover, we'd need to pass the full ID. But `InfoCard`'s `title` is set to `value` when it's a string, so we need a small tweak: pass the full ID as a custom node that includes a title.

Actually, looking at InfoCard more carefully — it sets `title={isString ? value : undefined}`. So if we truncate, the title will be the truncated value, not the full ID. To fix this, render a `<span>` with a title:

```tsx
import InfoCard from "../InfoCard";

export default function ResourceId({
  label,
  id,
  truncate,
}: {
  label: string;
  id?: string;
  truncate?: number;
}) {
  if (!id) return null;
  if (truncate && id.length > truncate) {
    return (
      <InfoCard
        label={label}
        value={<span className="font-mono" title={id}>{id.slice(0, truncate)}</span>}
      />
    );
  }
  return <InfoCard label={label} value={id} />;
}
```

**Step 2: Verify it type-checks**

Run: `cd frontend && npx tsc -b --noEmit`

**Step 3: Commit**

```
feat: add ResourceId metadata card component
```

---

### Task 2: Create `ResourceLink` component

**Files:**
- Create: `frontend/src/components/data/ResourceLink.tsx`

**Step 1: Create the component**

```tsx
import InfoCard from "../InfoCard";

export default function ResourceLink({
  label,
  name,
  to,
}: {
  label: string;
  name?: string;
  to: string;
}) {
  if (!name) return null;
  return <InfoCard label={label} value={name} href={to} />;
}
```

**Step 2: Verify it type-checks**

Run: `cd frontend && npx tsc -b --noEmit`

**Step 3: Commit**

```
feat: add ResourceLink metadata card component
```

---

### Task 3: Create `ContainerImage` component

**Files:**
- Create: `frontend/src/components/data/ContainerImage.tsx`

**Step 1: Create the component**

Uses `imageRegistryUrl()` to get the registry link. Shows a registry favicon inline.

```tsx
import InfoCard from "../InfoCard";
import {imageRegistryUrl} from "../../lib/imageUrl";

function registryFavicon(image: string): string | null {
  const segments = image.split("@")[0].split(":")[0].split("/");
  const first = segments[0];
  const isRegistry = first.includes(".") || first.includes(":");

  if (!isRegistry || first === "docker.io" || first === "registry-1.docker.io") {
    return "https://hub.docker.com/favicon.ico";
  }
  if (first === "ghcr.io") return "https://github.com/favicon.ico";
  if (first === "quay.io") return "https://quay.io/static/img/quay_favicon.png";
  if (first === "gcr.io" || first.endsWith(".gcr.io")) return "https://cloud.google.com/favicon.ico";
  return null;
}

export default function ContainerImage({
  image,
  label = "Image",
}: {
  image?: string;
  label?: string;
}) {
  if (!image) return null;
  const display = image.split("@")[0];
  const href = imageRegistryUrl(image) ?? undefined;
  const favicon = registryFavicon(image);

  return (
    <InfoCard
      label={label}
      value={
        <span className="inline-flex items-center gap-1.5">
          {favicon && <img src={favicon} alt="" className="h-4 w-4 shrink-0" />}
          {display}
        </span>
      }
      href={href}
    />
  );
}
```

Wait — `InfoCard` only renders the link when `value` is a string. Since we're passing a ReactNode (span with icon), the link won't render. We need to handle the link inside the component instead:

```tsx
import {Link} from "react-router-dom";
import InfoCard from "../InfoCard";
import {imageRegistryUrl} from "../../lib/imageUrl";

function registryFavicon(image: string): string | null {
  const segments = image.split("@")[0].split(":")[0].split("/");
  const first = segments[0];
  const isRegistry = first.includes(".") || first.includes(":");

  if (!isRegistry || first === "docker.io" || first === "registry-1.docker.io") {
    return "https://hub.docker.com/favicon.ico";
  }
  if (first === "ghcr.io") return "https://github.com/favicon.ico";
  if (first === "quay.io") return "https://quay.io/static/img/quay_favicon.png";
  if (first === "gcr.io" || first.endsWith(".gcr.io")) return "https://cloud.google.com/favicon.ico";
  return null;
}

export default function ContainerImage({
  image,
  label = "Image",
}: {
  image?: string;
  label?: string;
}) {
  if (!image) return null;
  const display = image.split("@")[0];
  const href = imageRegistryUrl(image);
  const favicon = registryFavicon(image);

  const content = (
    <span className="inline-flex items-center gap-1.5">
      {favicon && <img src={favicon} alt="" className="h-4 w-4 shrink-0" />}
      {display}
    </span>
  );

  return (
    <InfoCard
      label={label}
      value={
        href ? (
          <a href={href} target="_blank" rel="noopener noreferrer" className="text-link hover:underline">
            {content}
          </a>
        ) : (
          content
        )
      }
    />
  );
}
```

**Step 2: Verify it type-checks**

Run: `cd frontend && npx tsc -b --noEmit`

**Step 3: Commit**

```
feat: add ContainerImage metadata card component
```

---

### Task 4: Create `Timestamp` component

**Files:**
- Create: `frontend/src/components/data/Timestamp.tsx`

**Step 1: Create the component**

```tsx
import InfoCard from "../InfoCard";
import TimeAgo from "../TimeAgo";

export default function Timestamp({
  label,
  date,
  relative = true,
}: {
  label: string;
  date?: string;
  relative?: boolean;
}) {
  if (!date) return null;
  return (
    <InfoCard
      label={label}
      value={relative ? <TimeAgo date={date} /> : new Date(date).toLocaleString()}
    />
  );
}
```

**Step 2: Verify it type-checks**

Run: `cd frontend && npx tsc -b --noEmit`

**Step 3: Commit**

```
feat: add Timestamp metadata card component
```

---

### Task 5: Create barrel export

**Files:**
- Create: `frontend/src/components/data/index.ts`

**Step 1: Create barrel**

```ts
export { default as ResourceId } from "./ResourceId";
export { default as ResourceLink } from "./ResourceLink";
export { default as ContainerImage } from "./ContainerImage";
export { default as Timestamp } from "./Timestamp";
```

**Step 2: Verify it type-checks**

Run: `cd frontend && npx tsc -b --noEmit`

**Step 3: Commit**

```
feat: add barrel export for data card components
```

---

### Task 6: Replace InfoCard usages in TaskDetail

**Files:**
- Modify: `frontend/src/pages/TaskDetail.tsx`

**Step 1: Replace usages**

Replace imports — remove `InfoCard` and `timeAgo`, add data components:

```tsx
// Remove:
import InfoCard from "../components/InfoCard";
import { timeAgo } from "../components/TimeAgo";

// Add:
import InfoCard from "../components/InfoCard";
import { ResourceId, ResourceLink, ContainerImage, Timestamp } from "../components/data";
```

Keep `InfoCard` for non-replaceable usages (Desired State, Slot, Exit Code).

Replace the metadata grid cards (lines 58-74):

```tsx
<InfoCard label="Desired State" value={task.DesiredState} />
<ResourceLink label="Service" name={serviceName} to={`/services/${task.ServiceID}`} />
<ResourceLink label="Node" name={nodeLabel} to={`/nodes/${task.NodeID}`} />
<InfoCard label="Slot" value={task.Slot ? String(task.Slot) : "\u2014"} />
<ContainerImage image={task.Spec.ContainerSpec.Image} />
<Timestamp label="Timestamp" date={task.Status.Timestamp} />
{containerId && <ResourceId label="Container" id={containerId} truncate={12} />}
{exitCode != null && exitCode !== 0 && (
  <InfoCard label="Exit Code" value={String(exitCode)} />
)}
```

**Step 2: Verify it type-checks and tests pass**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run`

**Step 3: Commit**

```
refactor: use data card components in TaskDetail
```

---

### Task 7: Replace InfoCard usages in ServiceDetail

**Files:**
- Modify: `frontend/src/pages/ServiceDetail.tsx`

**Step 1: Replace usages**

Add import:
```tsx
import { ResourceLink, ContainerImage, Timestamp } from "../components/data";
```

Replace in overview cards section (lines 76-107):

- `<InfoCard label="Image" value={cs.Image.split("@")[0]} href={imageRegistryUrl(cs.Image) ?? undefined}/>` → `<ContainerImage image={cs.Image} />`
- `<InfoCard label="Stack" value={...} href={...} />` → `<ResourceLink label="Stack" name={labels["com.docker.stack.namespace"]} to={`/stacks/${labels["com.docker.stack.namespace"]}`} />`
- `{service.CreatedAt && <InfoCard label="Created" value={timeAgo(service.CreatedAt)}/>}` → `<Timestamp label="Created" date={service.CreatedAt} />`
- `{service.UpdatedAt && <InfoCard label="Updated" value={timeAgo(service.UpdatedAt)}/>}` → `<Timestamp label="Updated" date={service.UpdatedAt} />`

Remove `imageRegistryUrl` import if no longer used. Remove `timeAgo` import from TimeAgo (check if still used by Update Status card — yes it is, keep it).

**Step 2: Verify it type-checks and tests pass**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run`

**Step 3: Commit**

```
refactor: use data card components in ServiceDetail
```

---

### Task 8: Replace InfoCard usages in NodeDetail

**Files:**
- Modify: `frontend/src/pages/NodeDetail.tsx`

**Step 1: Replace usages**

Add import:
```tsx
import { Timestamp } from "../components/data";
```

NodeDetail doesn't have timestamps currently (no CreatedAt field on Node). But it does have InfoCards for Role, Status, Availability, etc. — these are plain key-value cards that don't match any of our new components. No replacements needed here beyond what was already done for task table links.

Actually — no changes needed for NodeDetail's InfoCards. The task table links were already updated in the previous enrichment work. Skip this task.

**Step 2: Verify unchanged**

Run: `cd frontend && npx tsc -b --noEmit`

---

### Task 9: Replace InfoCard usages in ConfigDetail

**Files:**
- Modify: `frontend/src/pages/ConfigDetail.tsx`

**Step 1: Replace usages**

Replace imports:
```tsx
// Remove:
import InfoCard from "../components/InfoCard";
import TimeAgo from "../components/TimeAgo";

// Add:
import { ResourceId, ResourceLink, Timestamp } from "../components/data";
```

Replace in metadata grid (lines 65-76):

```tsx
<div className="grid grid-cols-1 md:grid-cols-2 gap-4">
  <ResourceId label="ID" id={config.ID} />
  <ResourceLink label="Stack" name={stack} to={`/stacks/${stack}`} />
  <Timestamp label="Created" date={config.CreatedAt} />
  <Timestamp label="Updated" date={config.UpdatedAt} />
</div>
```

The `ResourceLink` for Stack handles `name={undefined}` by returning null, so no conditional needed.

**Step 2: Verify it type-checks and tests pass**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run`

**Step 3: Commit**

```
refactor: use data card components in ConfigDetail
```

---

### Task 10: Replace InfoCard usages in SecretDetail

**Files:**
- Modify: `frontend/src/pages/SecretDetail.tsx`

**Step 1: Replace usages**

Same pattern as ConfigDetail. Replace imports and metadata grid (lines 60-71):

```tsx
// Remove:
import InfoCard from "../components/InfoCard";
import TimeAgo from "../components/TimeAgo";

// Add:
import { ResourceId, ResourceLink, Timestamp } from "../components/data";
```

```tsx
<div className="grid grid-cols-1 md:grid-cols-2 gap-4">
  <ResourceId label="ID" id={secret.ID} />
  <ResourceLink label="Stack" name={stack} to={`/stacks/${stack}`} />
  <Timestamp label="Created" date={secret.CreatedAt} />
  <Timestamp label="Updated" date={secret.UpdatedAt} />
</div>
```

**Step 2: Verify it type-checks and tests pass**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run`

**Step 3: Commit**

```
refactor: use data card components in SecretDetail
```

---

### Task 11: Replace InfoCard usages in NetworkDetail

**Files:**
- Modify: `frontend/src/pages/NetworkDetail.tsx`

**Step 1: Replace usages**

Add import:
```tsx
import { ResourceId, ResourceLink, Timestamp } from "../components/data";
```

Replace in metadata grid (lines 127-135):

```tsx
<div className="grid grid-cols-1 md:grid-cols-2 gap-4">
  <ResourceId label="ID" id={network.Id} />
  <InfoCard label="Driver" value={network.Driver} />
  <InfoCard label="Scope" value={network.Scope} />
  <ResourceLink label="Stack" name={stack} to={`/stacks/${stack}`} />
  <Timestamp label="Created" date={network.Created} />
</div>
```

Remove `TimeAgo` import. Keep `InfoCard` import for Driver/Scope.

**Step 2: Verify it type-checks and tests pass**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run`

**Step 3: Commit**

```
refactor: use data card components in NetworkDetail
```

---

### Task 12: Replace InfoCard usages in VolumeDetail

**Files:**
- Modify: `frontend/src/pages/VolumeDetail.tsx`

**Step 1: Replace usages**

Add import:
```tsx
import { ResourceLink, Timestamp } from "../components/data";
```

Replace in metadata grid (lines 56-64):

```tsx
<div className="grid grid-cols-1 md:grid-cols-2 gap-4">
  <InfoCard label="Driver" value={volume.Driver} />
  <InfoCard label="Scope" value={volume.Scope} />
  <InfoCard label="Mountpoint" value={volume.Mountpoint} />
  <ResourceLink label="Stack" name={stack} to={`/stacks/${stack}`} />
  <Timestamp label="Created" date={volume.CreatedAt} />
</div>
```

Remove `TimeAgo` import. Keep `InfoCard` for Driver/Scope/Mountpoint.

**Step 2: Verify it type-checks and tests pass**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run`

**Step 3: Commit**

```
refactor: use data card components in VolumeDetail
```

---

### Task 13: Final verification

**Step 1: Run full check**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run && npm run lint`

**Step 2: Verify no unused imports of InfoCard or TimeAgo remain where replaced**

Grep for leftover imports in modified files to confirm cleanup.

**Step 3: Final commit if any cleanup needed**
