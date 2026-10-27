import { type AttestedKind, api, attestationPath } from "@/api/client";
import type {
  AttestationStatus,
  ImageProvenance,
  PlatformProvenance,
  ProvenanceCI,
  ProvenanceSummary,
} from "@/api/types";
import CollapsibleSection from "@/components/CollapsibleSection";
import { Button } from "@/components/ui/button";
import { formatBytes, formatDuration } from "@/lib/format";
import { useQuery } from "@tanstack/react-query";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";

/**
 * Plain statements of why an image shows no build information. None of them
 * is an error in Cetacean, so none is styled as one.
 */
const statusLines: Record<Exclude<AttestationStatus, "available" | "disabled">, string> = {
  none: "This image carries no build attestations.",
  unpinned: "The image isn't pinned by digest, so its attestations can't be looked up.",
  private: "The registry doesn't allow anonymous access to this image.",
  missing: "The registry no longer has this image.",
  unavailable: "The registry couldn't be reached.",
};

/**
 * What the provenance BuildKit attached to a service's or task's image says
 * about how it was built. It loads once the section scrolls into view, since
 * reading it asks the image's registry.
 */
export default function BuildSection({ kind, id }: { kind: AttestedKind; id: string }) {
  const anchor = useRef<HTMLDivElement>(null);
  const visible = useInView(anchor);
  const { data, refetch } = useQuery({
    queryKey: ["provenance", kind, id],
    queryFn: ({ signal }) => api.provenance(kind, id, signal),
    enabled: visible,
  });

  if (data?.status === "disabled") {
    return null;
  }

  return (
    <div ref={anchor}>
      {data && (
        <CollapsibleSection title="Build">
          <p className="mb-3 text-xs text-muted-foreground">As recorded by the image's build</p>
          {data.status === "available" ? (
            <Platforms
              kind={kind}
              id={id}
              provenance={data}
            />
          ) : (
            <StatusLine
              key={data.retryAfter}
              provenance={data}
              onRetry={() => refetch()}
            />
          )}
        </CollapsibleSection>
      )}
    </div>
  );
}

function useInView(anchor: React.RefObject<Element | null>) {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    if (visible || !anchor.current) {
      return;
    }

    const observer = new IntersectionObserver((entries) => {
      if (entries.some(({ isIntersecting }) => isIntersecting)) {
        setVisible(true);
      }
    });

    observer.observe(anchor.current);

    return () => observer.disconnect();
  }, [anchor, visible]);

  return visible;
}

function StatusLine({
  provenance: { status, retryAfter },
  onRetry,
}: {
  provenance: ImageProvenance;
  onRetry: () => void;
}) {
  const ready = useTimePassed(status === "unavailable" ? retryAfter : undefined);
  const line = statusLines[status as keyof typeof statusLines];

  return (
    <div
      data-testid="build-status"
      className="flex items-center gap-3 text-sm text-muted-foreground"
    >
      <span>{line}</span>
      {status === "unavailable" && (
        <Button
          variant="outline"
          size="sm"
          disabled={!ready}
          onClick={onRetry}
        >
          Retry
        </Button>
      )}
    </div>
  );
}

/**
 * Whether a moment has passed, re-rendering when it does. The caller keys the
 * component on the timestamp, so a new one starts from fresh state.
 */
function useTimePassed(timestamp: string | undefined) {
  const [passed, setPassed] = useState(() => !timestamp || Date.parse(timestamp) <= Date.now());

  useEffect(() => {
    if (!timestamp) {
      return;
    }

    const wait = Date.parse(timestamp) - Date.now();
    if (wait <= 0) {
      return;
    }

    const timer = setTimeout(() => setPassed(true), wait);

    return () => clearTimeout(timer);
  }, [timestamp]);

  return passed;
}

/**
 * The platform most of a service's running tasks are on is the one a reader
 * most likely means; a task offers only its own.
 */
function defaultPlatform(platforms: PlatformProvenance[]) {
  return platforms.reduce(
    (best, candidate) => (candidate.runningTasks > best.runningTasks ? candidate : best),
    platforms[0] as PlatformProvenance,
  ).platform;
}

function Platforms({
  kind,
  id,
  provenance,
}: {
  kind: AttestedKind;
  id: string;
  provenance: ImageProvenance;
}) {
  const platforms = provenance.platforms ?? [];
  const [selected, setSelected] = useState(() =>
    platforms.length > 0 ? defaultPlatform(platforms) : "",
  );
  const current = platforms.find(({ platform }) => platform === selected) ?? platforms[0];

  if (!current) {
    return <p className="text-sm text-muted-foreground">{statusLines.none}</p>;
  }

  return (
    <div className="flex flex-col gap-3 text-sm">
      {platforms.length > 1 && (
        <label className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
          Platform
          <select
            aria-label="Platform"
            value={current.platform}
            onChange={(event) => setSelected(event.target.value)}
            className="flex h-8 rounded-md border border-input bg-transparent px-3 text-sm text-foreground outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
          >
            {platforms.map(({ platform }) => (
              <option
                key={platform}
                value={platform}
              >
                {platform}
              </option>
            ))}
          </select>
        </label>
      )}

      {current.provenance ? (
        <Summary provenance={current.provenance} />
      ) : (
        <p className="text-muted-foreground">No provenance for this platform.</p>
      )}

      <div className="flex flex-wrap gap-2">
        {current.sbom && !current.sbom.tooLarge && (
          <Link
            className="underline-offset-4 hover:underline"
            to={attestationPath(kind, id, "sbom", current.platform)}
          >
            SBOM ({formatBytes(current.sbom.size)})
          </Link>
        )}
        {current.sbom?.tooLarge && (
          <span className="text-muted-foreground">The SBOM is too large to show here.</span>
        )}
        {current.dockerfile && (
          <Link
            className="underline-offset-4 hover:underline"
            to={attestationPath(kind, id, "dockerfile", current.platform)}
          >
            Dockerfile with layer sizes
          </Link>
        )}
      </div>
    </div>
  );
}

function Summary({ provenance }: { provenance: ProvenanceSummary }) {
  const { source, builder, ci, dockerfilePath, startedOn, finishedOn, inputs } = provenance;
  const images = (inputs ?? []).filter(({ image }) => image);
  const built = finishedOn ? new Date(finishedOn).toLocaleString() : undefined;
  const took =
    startedOn && finishedOn
      ? formatDuration((Date.parse(finishedOn) - Date.parse(startedOn)) * 1_000_000)
      : undefined;

  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5">
      {source && (
        <Row label="Source">
          <ExternalOrText href={source.repositoryUrl}>{source.repository}</ExternalOrText>
          {source.subdirectory && (
            <span className="text-muted-foreground"> /{source.subdirectory}</span>
          )}
          {source.commit && (
            <>
              {" @ "}
              <ExternalOrText href={source.commitUrl}>
                <code>{source.commit.slice(0, 12)}</code>
              </ExternalOrText>
            </>
          )}
          {source.ref && source.ref !== source.commit && (
            <span className="text-muted-foreground"> ({source.ref})</span>
          )}
        </Row>
      )}
      {ci && (
        <Row label="CI">
          <CILine ci={ci} />
        </Row>
      )}
      {builder && (
        <Row label="Builder">
          <ExternalOrText href={builder.href}>{builder.value}</ExternalOrText>
        </Row>
      )}
      {built && (
        <Row label="Built">
          {built}
          {took && <span className="text-muted-foreground"> in {took}</span>}
        </Row>
      )}
      {dockerfilePath && (
        <Row label="Dockerfile">
          <code>{dockerfilePath}</code>
        </Row>
      )}
      {images.length > 0 && (
        <Row label="Inputs">
          <ul>
            {images.map(({ uri, image }) => (
              <li
                key={uri}
                className="break-all"
              >
                <code>{image}</code>
              </li>
            ))}
          </ul>
        </Row>
      )}
    </dl>
  );
}

function CILine({ ci }: { ci: ProvenanceCI }) {
  const workflow = ci.workflow?.split("/").pop();

  return (
    <span>
      {ci.event === "push" ? "Pushed" : "Built"}
      {ci.actor && ` by ${ci.actor}`}
      {ci.ref && (
        <>
          {" to "}
          <code>{ci.ref}</code>
        </>
      )}
      {workflow && (
        <>
          {" · workflow "}
          <ExternalOrText href={ci.workflowUrl}>{workflow}</ExternalOrText>
        </>
      )}
      {ci.runId && (
        <>
          {" · "}
          <ExternalOrText href={ci.runUrl}>run {ci.runId}</ExternalOrText>
        </>
      )}
    </span>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </>
  );
}

function ExternalOrText({ href, children }: { href?: string | undefined; children: ReactNode }) {
  if (!href) {
    return <>{children}</>;
  }

  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="underline-offset-4 hover:underline"
    >
      {children}
    </a>
  );
}
