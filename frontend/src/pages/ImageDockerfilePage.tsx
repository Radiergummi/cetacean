import { type AttestedKind, api, attestationPath } from "@/api/client";
import type { ImageDockerfile } from "@/api/types";
import FetchError from "@/components/FetchError";
import { LoadingDetail } from "@/components/LoadingSkeleton";
import PageHeader from "@/components/PageHeader";
import { buttonVariants } from "@/components/ui/button";
import { useAttestationBreadcrumbs } from "@/hooks/useAttestationBreadcrumbs";
import { apiPath } from "@/lib/basePath";
import { formatBytes } from "@/lib/format";
import { highlightLines } from "@/lib/highlight";
import { toneBanner } from "@/lib/statusColor";
import { getErrorMessage } from "@/lib/utils";
import { useQuery } from "@tanstack/react-query";
import { Info } from "lucide-react";
import { useEffect, useMemo } from "react";
import { Link, useLocation, useParams, useSearchParams } from "react-router-dom";

/**
 * The bytes each line produced, on the first line of each run of adjacent
 * ranges its step was written on: BuildKit records a multi-line RUN one line
 * at a time. A FROM two stages share shows its layers on both lines.
 */
function sizesByLine(dockerfile: ImageDockerfile) {
  const sizes = new Map<number, number>();

  for (const { ranges, layers } of dockerfile.steps ?? []) {
    const size = (layers ?? []).reduce((sum, { size: layerSize }) => sum + layerSize, 0);
    let previousEnd = -Infinity;

    for (const [start, end] of ranges) {
      if (start > previousEnd + 1) {
        sizes.set(start, (sizes.get(start) ?? 0) + size);
      }

      previousEnd = Math.max(previousEnd, end);
    }
  }

  return sizes;
}

function formatShare(share: number) {
  return share > 0 && share < 0.001 ? "<0.1%" : `${(share * 100).toFixed(1)}%`;
}

/**
 * A line's share of the image as a small pie. A line that produced any bytes
 * at all fills at least one degree, so it never reads as empty.
 */
function SharePie({ line, share }: { line: number; share: number }) {
  const degrees = share > 0 ? Math.max(1, share * 360) : 0;

  return (
    <span
      role="img"
      aria-label={`${formatShare(share)} of the image`}
      title={`${formatShare(share)} of the image`}
      data-testid={`pie-${line}`}
      data-degrees={degrees}
      className="size-3 shrink-0 rounded-full"
      style={{
        background: `conic-gradient(var(--color-primary) ${degrees}deg, var(--color-muted) 0)`,
      }}
    />
  );
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
  const highlighted = useMemo(
    () => (data ? highlightLines(data.lines.join("\n"), "dockerfile") : []),
    [data],
  );
  const breadcrumbs = useAttestationBreadcrumbs(kind, id, "Dockerfile");
  const { hash, search } = useLocation();

  // The line is rendered only once the file has loaded, too late for the
  // browser's own scroll to the fragment.
  useEffect(() => {
    if (data && hash) {
      document.getElementById(hash.slice(1))?.scrollIntoView({ block: "nearest" });
    }
  }, [data, hash]);

  if (error) {
    return (
      <>
        <PageHeader
          title="Dockerfile"
          breadcrumbs={breadcrumbs}
        />
        <FetchError
          message={getErrorMessage(error, "Failed to load the Dockerfile")}
          onRetry={() => refetch()}
        />
      </>
    );
  }

  if (isLoading || !data) {
    return <LoadingDetail />;
  }

  const unattributed = data.unattributed ?? [];
  const total = Math.max(1, data.totalSize);

  return (
    <>
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

      <div className="overflow-x-auto rounded-lg border bg-card font-mono text-xs [--targeted-line:color-mix(in_oklab,var(--status-warning)_18%,var(--card))]">
        <table className="w-full border-separate border-spacing-0">
          <tbody>
            {data.lines.map((text, index) => {
              const line = index + 1;
              const size = sizes.get(line);

              return (
                <tr
                  key={line}
                  id={`L${line}`}
                  data-testid={`line-${line}`}
                  data-targeted={hash === `#L${line}` || undefined}
                  className="group scroll-mt-20 align-top"
                >
                  <td className="sticky left-0 border-r bg-card px-2 py-0.5 text-muted-foreground select-none group-data-targeted:bg-(--targeted-line)">
                    <div className="flex items-center gap-2">
                      <Link
                        to={{ search, hash: `L${line}` }}
                        className="w-8 text-right hover:text-foreground hover:underline"
                      >
                        {line}
                      </Link>
                      <span className="flex w-20 items-center gap-1.5 whitespace-nowrap">
                        {size !== undefined && (
                          <>
                            <SharePie
                              line={line}
                              share={size / total}
                            />
                            {formatBytes(size)}
                          </>
                        )}
                      </span>
                    </div>
                  </td>
                  <td className="px-3 py-0.5 whitespace-pre group-data-targeted:bg-(--targeted-line)">
                    {highlighted[index] ?? text}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {unattributed.length > 0 && (
        <div
          className={`mt-4 flex items-start gap-3 rounded-lg border px-4 py-3 text-sm ${toneBanner.info}`}
        >
          <Info
            className="mt-0.5 size-4 shrink-0"
            aria-hidden="true"
          />
          <div className="min-w-0">
            <p className="mb-2 font-medium">
              Layers no line of this Dockerfile produced, as far as the provenance says
            </p>
            <ul className="font-mono text-xs break-all">
              {unattributed.map(({ digest, size }) => (
                <li key={digest}>
                  {digest} · {formatBytes(size)}
                </li>
              ))}
            </ul>
          </div>
        </div>
      )}
    </>
  );
}
