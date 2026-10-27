import { type AttestedKind, api, attestationPath } from "@/api/client";
import type { ImageDockerfile } from "@/api/types";
import FetchError from "@/components/FetchError";
import { LoadingDetail } from "@/components/LoadingSkeleton";
import PageHeader from "@/components/PageHeader";
import { buttonVariants } from "@/components/ui/button";
import { apiPath } from "@/lib/basePath";
import { formatBytes } from "@/lib/format";
import { getErrorMessage } from "@/lib/utils";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { useParams, useSearchParams } from "react-router-dom";

/**
 * The bytes each line produced, on the first line of each range its step was
 * written on. A step written on two lines, such as a FROM two stages share,
 * shows its layers on both; the totals count them once.
 */
function sizesByLine(dockerfile: ImageDockerfile) {
  const sizes = new Map<number, number>();

  for (const { ranges, layers } of dockerfile.steps ?? []) {
    const size = (layers ?? []).reduce((sum, { size: layerSize }) => sum + layerSize, 0);

    for (const [start] of ranges) {
      sizes.set(start, (sizes.get(start) ?? 0) + size);
    }
  }

  return sizes;
}

/**
 * The Dockerfile an image's mode=max provenance embeds, with the size of the
 * layers each instruction produced beside it.
 */
export default function ImageDockerfilePage({ kind }: { kind: AttestedKind }) {
  const { id = "" } = useParams<{ id: string }>();
  const [searchParams] = useSearchParams();
  const platform = searchParams.get("platform") ?? undefined;

  const { data, error, isLoading, refetch } = useQuery({
    queryKey: ["dockerfile", kind, id, platform],
    queryFn: ({ signal }) => api.dockerfile(kind, id, platform, signal),
  });

  const sizes = useMemo(() => (data ? sizesByLine(data) : new Map<number, number>()), [data]);
  const heaviest = Math.max(1, ...sizes.values());
  const back = `/${kind}s/${id}`;
  const breadcrumbs = [
    { label: kind === "service" ? "Service" : "Task", to: back },
    { label: "Dockerfile" },
  ];

  if (error) {
    return (
      <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
        <PageHeader
          title="Dockerfile"
          breadcrumbs={breadcrumbs}
        />
        <FetchError
          message={getErrorMessage(error, "Failed to load the Dockerfile")}
          onRetry={() => refetch()}
        />
      </div>
    );
  }

  if (isLoading || !data) {
    return (
      <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
        <LoadingDetail />
      </div>
    );
  }

  const unattributed = data.unattributed ?? [];

  return (
    <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
      <PageHeader
        title={data.path || "Dockerfile"}
        subtitle={`${formatBytes(data.totalSize)} on ${data.platform}, ${formatBytes(data.baseSize)} from the base image, as the image's provenance records it.`}
        breadcrumbs={breadcrumbs}
        actions={
          <a
            href={apiPath(attestationPath(kind, id, "dockerfile", data.platform, ".txt"))}
            download="Dockerfile"
            className={buttonVariants({ variant: "outline", size: "sm" })}
          >
            Download
          </a>
        }
      />

      <div className="overflow-x-auto rounded-lg border bg-card font-mono text-xs">
        <table className="w-full border-collapse">
          <tbody>
            {data.lines.map((text, index) => {
              const line = index + 1;
              const size = sizes.get(line);

              return (
                <tr
                  key={line}
                  data-testid={`line-${line}`}
                  className="align-top"
                >
                  <td className="w-10 px-2 py-0.5 text-right text-muted-foreground select-none">
                    {line}
                  </td>
                  <td className="w-40 px-2 py-0.5">
                    {size !== undefined && (
                      <div className="flex items-center gap-2">
                        <div className="h-2 w-20 shrink-0 rounded-sm bg-muted">
                          <div
                            data-testid={`bar-${line}`}
                            className="h-2 rounded-sm bg-primary"
                            style={{ width: `${(size / heaviest) * 100}%` }}
                          />
                        </div>
                        <span className="whitespace-nowrap text-muted-foreground">
                          {formatBytes(size)}
                        </span>
                      </div>
                    )}
                  </td>
                  <td className="px-2 py-0.5 whitespace-pre">{text}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {unattributed.length > 0 && (
        <div className="mt-4 text-sm">
          <p className="mb-2 text-muted-foreground">
            Layers no line of this Dockerfile produced, as far as the provenance says:
          </p>
          <ul className="font-mono text-xs">
            {unattributed.map(({ digest, size }) => (
              <li key={digest}>
                {digest} · {formatBytes(size)}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
