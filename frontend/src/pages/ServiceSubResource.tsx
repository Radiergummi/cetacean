import { get } from "@/api/client";
import FetchError from "@/components/FetchError";
import { LoadingDetail } from "@/components/LoadingSkeleton";
import PageHeader from "@/components/PageHeader";
import ResourceName from "@/components/ResourceName";
import SimpleTable from "@/components/SimpleTable";
import { useEffect, useState } from "react";
import { Navigate, useParams } from "react-router-dom";

const subResources: Record<string, string> = {
  env: "Environment Variables",
  labels: "Labels",
  resources: "Resources",
  healthcheck: "Healthcheck",
  placement: "Placement",
  ports: "Ports",
  "update-policy": "Update Policy",
  "rollback-policy": "Rollback Policy",
  "log-driver": "Log Driver",
  configs: "Configs",
  secrets: "Secrets",
  networks: "Networks",
  mounts: "Mounts",
  "container-config": "Container Config",
};

export default function ServiceSubResource() {
  const { id, subResource } = useParams<{ id: string; subResource: string }>();

  const label = subResource ? subResources[subResource] : undefined;

  const [data, setData] = useState<unknown>(null);
  const [serviceName, setServiceName] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [retryCount, setRetryCount] = useState(0);

  useEffect(() => {
    if (!id || !subResource || !label) {
      return;
    }

    setLoading(true);
    setError(null);

    const controller = new AbortController();
    const signal = controller.signal;

    Promise.all([
      get<unknown>(`/services/${id}/${subResource}`, signal).then(({ data }) => data),
      get<{ service?: { Spec?: { Name?: string } } }>(`/services/${id}`, signal)
        .then(({ data }) => data)
        .catch(() => null),
    ])
      .then(([subData, serviceData]) => {
        setData(subData);
        setServiceName(serviceData?.service?.Spec?.Name ?? null);
        setLoading(false);
      })
      .catch((fetchError) => {
        if (!signal.aborted) {
          setError(fetchError.message || "Failed to load");
          setLoading(false);
        }
      });

    return () => controller.abort();
  }, [id, subResource, label, retryCount]);

  if (!label) {
    return <Navigate to={`/services/${id}`} />;
  }

  if (loading) {
    return <LoadingDetail />;
  }

  const displayName = serviceName ?? id!;

  const header = (
    <PageHeader
      title={label}
      breadcrumbs={[
        { label: "Services", to: "/services" },
        { label: <ResourceName name={displayName} />, to: `/services/${id}` },
        { label },
      ]}
    />
  );

  if (error) {
    return (
      <>
        {header}
        <FetchError
          message={error}
          onRetry={() => setRetryCount((count) => count + 1)}
        />
      </>
    );
  }

  return (
    <>
      {header}
      <DataView data={data} />
    </>
  );
}

/**
 * Strips JSON-LD keys (@context, @id, @type) and unwraps single-key wrapper
 * objects (e.g., { env: { FOO: "bar" } } → { FOO: "bar" }).
 */
function unwrapResponse(object: Record<string, unknown>): unknown {
  const entries = Object.entries(object).filter(([key]) => !key.startsWith("@"));

  if (entries.length === 1) {
    return entries[0][1];
  }

  return Object.fromEntries(entries);
}

function DataView({ data }: { data: unknown }) {
  if (data === null || data === undefined) {
    return <EmptyState />;
  }

  if (Array.isArray(data)) {
    return data.length === 0 ? <EmptyState /> : <ArrayView items={data} />;
  }

  if (typeof data === "object") {
    const inner = unwrapResponse(data as Record<string, unknown>);

    if (inner === null || inner === undefined) {
      return <EmptyState />;
    }

    if (Array.isArray(inner)) {
      return inner.length === 0 ? <EmptyState /> : <ArrayView items={inner} />;
    }

    if (typeof inner === "object") {
      const entries = Object.entries(inner as Record<string, unknown>);

      return entries.length === 0 ? <EmptyState /> : <KeyValueTable entries={entries} />;
    }

    return <pre className="rounded-lg border bg-muted/30 p-4 text-sm">{String(inner)}</pre>;
  }

  return <pre className="rounded-lg border bg-muted/30 p-4 text-sm">{String(data)}</pre>;
}

function KeyValueTable({ entries }: { entries: [string, unknown][] }) {
  return (
    <SimpleTable
      columns={["Key", "Value"]}
      items={entries}
      keyFn={([key]) => key}
      renderRow={([key, value]) => (
        <>
          <td className="p-3 align-top font-mono text-sm font-medium">{key}</td>
          <td className="p-3 align-top font-mono text-sm text-muted-foreground">
            <ValueCell value={value} />
          </td>
        </>
      )}
    />
  );
}

function ArrayView({ items }: { items: unknown[] }) {
  const first = items[0];

  if (typeof first !== "object" || first === null) {
    return (
      <SimpleTable
        columns={["Value"]}
        items={items}
        keyFn={(_, index) => index}
        renderRow={(item) => (
          <td className="p-3 font-mono text-sm">
            <ValueCell value={item} />
          </td>
        )}
      />
    );
  }

  const columns = Object.keys(first as Record<string, unknown>);

  return (
    <SimpleTable
      columns={columns}
      items={items as Record<string, unknown>[]}
      keyFn={(_, index) => index}
      renderRow={(item) => (
        <>
          {columns.map((column) => (
            <td
              key={column}
              className="p-3 font-mono text-sm"
            >
              <ValueCell value={item[column]} />
            </td>
          ))}
        </>
      )}
    />
  );
}

function ValueCell({ value }: { value: unknown }) {
  if (value === null || value === undefined) {
    return <span className="text-muted-foreground/50">—</span>;
  }

  if (typeof value === "boolean") {
    return <>{value ? "true" : "false"}</>;
  }

  if (typeof value === "object") {
    return (
      <pre className="max-w-lg text-xs break-all whitespace-pre-wrap">
        {JSON.stringify(value, null, 2)}
      </pre>
    );
  }

  return <>{String(value)}</>;
}

function EmptyState() {
  return (
    <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">
      No data
    </div>
  );
}
