# Toast Notifications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Sonner toast notifications for transient API errors, keeping inline errors only where actionable (force-remove).

**Architecture:** Install Sonner via shadcn, add `<Toaster />` to App.tsx, extend `useAsyncAction` with a `toast` option, and switch mutation callers from inline error text to toast notifications. Inline errors stay for `RemoveResourceAction` and `NodeActions` (force-remove flow).

**Tech Stack:** Sonner (via shadcn/ui), React 19, TypeScript

**Spec:** `docs/superpowers/specs/2026-03-24-toast-notifications-design.md`

---

### Task 1: Install Sonner and add Toaster to App

**Files:**
- Create: `frontend/src/components/ui/sonner.tsx` (shadcn generates this)
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/package.json` (dependency added by shadcn)

- [ ] **Step 1: Install Sonner via shadcn**

```bash
cd frontend && npx shadcn@latest add sonner
```

- [ ] **Step 2: Add Toaster to App.tsx**

In `App.tsx`, import the Toaster and add it inside `ConnectionTracker`, before `Layout`:

```tsx
import { Toaster } from "@/components/ui/sonner";
```

```tsx
// In the App component return:
<ConnectionTracker>
  <Toaster
    theme="system"
    richColors
    position="bottom-right"
    toastOptions={{ duration: 8000 }}
  />
  <Layout>
```

- [ ] **Step 3: Verify it compiles**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/ui/sonner.tsx frontend/src/App.tsx frontend/package.json frontend/package-lock.json
git commit -m "feat: add Sonner toast infrastructure"
```

---

### Task 2: Add toast helper and extend useAsyncAction

**Files:**
- Create: `frontend/src/lib/showErrorToast.ts`
- Modify: `frontend/src/hooks/useAsyncAction.ts`

- [ ] **Step 1: Create showErrorToast helper**

Create `frontend/src/lib/showErrorToast.ts`:

```ts
import { ApiError } from "@/api/client";
import { getErrorInfo } from "@/lib/errors";
import { toast } from "sonner";

/**
 * Show a toast notification for an API error.
 * Uses the error dictionary for known error codes,
 * falls back to the error message for unknown errors.
 */
export function showErrorToast(error: unknown, fallback: string): void {
  const code = error instanceof ApiError ? error.code : null;
  const info = getErrorInfo(code);

  if (info) {
    toast.error(info.title, { description: info.suggestion });
  } else {
    const message = error instanceof Error ? error.message : fallback;
    toast.error(message);
  }
}
```

- [ ] **Step 2: Extend useAsyncAction with toast option**

Modify `frontend/src/hooks/useAsyncAction.ts`:

```ts
import { getErrorMessage } from "@/lib/utils";
import { showErrorToast } from "@/lib/showErrorToast";
import { useEffect, useRef, useState } from "react";

interface AsyncActionOptions {
  toast?: boolean;
}

export function useAsyncAction(options?: AsyncActionOptions) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [cause, setCause] = useState<unknown>(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  async function execute(action: () => Promise<unknown>, errorMessage: string) {
    setLoading(true);
    setError(null);
    setCause(null);

    try {
      await action();
    } catch (caught) {
      if (mountedRef.current) {
        if (options?.toast) {
          showErrorToast(caught, errorMessage);
        } else {
          setError(getErrorMessage(caught, errorMessage));
          setCause(caught);
        }
      }
    } finally {
      if (mountedRef.current) {
        setLoading(false);
      }
    }
  }

  return { loading, error, cause, execute };
}
```

- [ ] **Step 3: Verify it compiles**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/showErrorToast.ts frontend/src/hooks/useAsyncAction.ts
git commit -m "feat: add toast option to useAsyncAction"
```

---

### Task 3: Switch mutation callers to toast mode

Switch all `useAsyncAction()` callers that show inline error text to `useAsyncAction({ toast: true })`, and remove their inline error display. Do NOT change `RemoveResourceAction` or `NodeActions` (they need inline errors for force-remove).

**Files to modify:**
- `frontend/src/components/service-detail/ServiceActions.tsx`
- `frontend/src/components/service-detail/ReplicaCard.tsx`
- `frontend/src/components/service-detail/EndpointModeEditor.tsx`
- `frontend/src/components/data/ContainerImage.tsx`
- `frontend/src/components/node-detail/AvailabilityEditor.tsx`
- `frontend/src/components/node-detail/RoleEditor.tsx`
- `frontend/src/components/swarm-detail/SwarmActions.tsx`
- `frontend/src/components/stack-detail/StackActions.tsx`
- `frontend/src/pages/SwarmPage.tsx`
- `frontend/src/pages/TaskDetail.tsx`
- `frontend/src/pages/PluginDetail.tsx`
- `frontend/src/components/InstallPluginDialog.tsx`

For each file, the pattern is:

1. Change `useAsyncAction()` to `useAsyncAction({ toast: true })`
2. Remove the inline error display (`{action.error && <p className="text-xs text-red-600 ...">...</p>}`)
3. Stop destructuring or passing `error` where it was only used for inline display

**ServiceActions.tsx** requires special attention: it has a `ConfirmAction` sub-component that accepts an `error` prop. With toast mode, the `error` will always be `null`, so `ConfirmAction` can keep the prop (it just won't render). However, the `remove` action navigates away on success — if removal fails with a non-force-removable error (like SVC002 for stack-managed services), the toast will show. This is correct.

**PluginDetail.tsx** has `enableAction`, `removeAction`, `upgradeAction`, and `configureAction`. The `configureAction` error is shown inside an `EditablePanel`-like inline editor. Check whether it uses `useAsyncAction` or `EditablePanel`'s own error handling. If it uses `useAsyncAction`, switch to toast.

- [ ] **Step 1: Convert all callers**

For each file listed above:
- Read the file
- Change `useAsyncAction()` to `useAsyncAction({ toast: true })`
- Remove inline error JSX that references the action's `.error` property (the `<p>` tag with `text-red-600`)
- If the component passes `.error` to a child (like `ConfirmAction`), keep passing it — it will just be `null`

- [ ] **Step 2: Verify it compiles and lint passes**

```bash
cd frontend && npx tsc -b --noEmit && npx oxlint .
```

- [ ] **Step 3: Commit**

```bash
git add -u frontend/src/
git commit -m "feat: switch mutation actions to toast error notifications"
```

---

### Task 4: Verify and clean up

- [ ] **Step 1: Run full Go test suite**

```bash
cd /Users/moritz/GolandProjects/cetacean && go test ./...
```

- [ ] **Step 2: Run frontend type check and lint**

```bash
cd frontend && npx tsc -b --noEmit && npx oxlint .
```

- [ ] **Step 3: Manual verification**

Verify the Vite dev server starts and the app loads:

```bash
cd frontend && npm run dev
```

- [ ] **Step 4: Commit any cleanup**

If any cleanup was needed, commit it.
