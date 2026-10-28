import { type AttestedKind, attestationPath } from "@/api/client";
import type {
  AttestationStatus,
  ImageProvenance,
  PlatformProvenance,
  ProvenanceCI,
  ProvenanceSummary,
} from "@/api/types";
import CollapsibleSection from "@/components/CollapsibleSection";
import { KVTable } from "@/components/data";
import { Button, buttonVariants } from "@/components/ui/button";
import { useProvenance } from "@/hooks/useProvenance";
import { formatBytes, formatDateTime, formatDuration } from "@/lib/format";
import { ExternalLink } from "lucide-react";
import { Fragment, type ReactNode, useEffect, useState } from "react";
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
 * about how it was built, with its SBOM and Dockerfile pages as actions.
 */
export default function BuildSection({ kind, id }: { kind: AttestedKind; id: string }) {
  const { data, refetch } = useProvenance(kind, id);
  const [selected, setSelected] = useState<string>();

  if (!data || data.status === "disabled") {
    return null;
  }

  const platforms = data.platforms ?? [];
  const current =
    data.status === "available"
      ? platforms.find(({ platform }) => platform === (selected ?? defaultPlatform(platforms)))
      : undefined;

  return (
    <CollapsibleSection
      title="Manifest"
      controls={
        current && (
          <Actions
            kind={kind}
            id={id}
            platforms={platforms}
            current={current}
            onSelect={setSelected}
          />
        )
      }
    >
      {data.status !== "available" ? (
        <StatusLine
          key={data.retryAfter}
          provenance={data}
          onRetry={() => refetch()}
        />
      ) : current ? (
        <Manifest
          kind={kind}
          id={id}
          platform={current}
        />
      ) : (
        <p className="text-sm text-muted-foreground">{statusLines.none}</p>
      )}
    </CollapsibleSection>
  );
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

function Actions({
  kind,
  id,
  platforms,
  current,
  onSelect,
}: {
  kind: AttestedKind;
  id: string;
  platforms: PlatformProvenance[];
  current: PlatformProvenance;
  onSelect: (platform: string) => void;
}) {
  return (
    <>
      {platforms.length > 1 && (
        <select
          aria-label="Platform"
          value={current.platform}
          onChange={(event) => onSelect(event.target.value)}
          className="flex h-8 rounded-md border border-input bg-transparent px-3 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
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
      )}
      {current.sbom && !current.sbom.tooLarge && (
        <Link
          to={attestationPath(kind, id, "sbom", current.platform)}
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          SBOM
        </Link>
      )}
      {current.dockerfile && (
        <Link
          to={attestationPath(kind, id, "dockerfile", current.platform)}
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          Show Dockerfile
        </Link>
      )}
    </>
  );
}

function Manifest({
  kind,
  id,
  platform,
}: {
  kind: AttestedKind;
  id: string;
  platform: PlatformProvenance;
}) {
  const { provenance, sbom, dockerfile } = platform;

  if (!provenance) {
    return <p className="text-sm text-muted-foreground">No provenance for this platform.</p>;
  }

  const { source, builder, ci, dockerfilePath, startedOn, finishedOn, inputs } = provenance;
  const images = (inputs ?? []).filter(({ image }) => image);
  const took =
    startedOn && finishedOn
      ? formatDuration((Date.parse(finishedOn) - Date.parse(startedOn)) * 1_000_000)
      : undefined;
  const dockerfileName = dockerfilePath || "Dockerfile";

  return (
    <KVTable
      rows={[
        source && [
          "Source",
          <SourceLine
            key="source"
            source={source}
          />,
          undefined,
          source.commit ? `${source.repository}@${source.commit}` : source.repository,
        ],
        ci && [
          "CI",
          <CILine
            key="ci"
            ci={ci}
          />,
          undefined,
          ci.runUrl ?? "",
        ],
        builder && [
          "Builder",
          <ExternalOrText
            key="builder"
            href={builder.href}
          >
            {builder.value}
          </ExternalOrText>,
          undefined,
          builder.value,
        ],
        finishedOn && [
          "Built",
          <Fragment key="built">
            {formatDateTime(finishedOn)}
            {took && <span className="text-muted-foreground"> in {took}</span>}
          </Fragment>,
          undefined,
          finishedOn,
        ],
        (dockerfile || dockerfilePath) && [
          "Dockerfile",
          dockerfile ? (
            <Link
              key="dockerfile"
              to={attestationPath(kind, id, "dockerfile", platform.platform)}
              className="text-link hover:underline"
            >
              {dockerfileName}
            </Link>
          ) : (
            dockerfileName
          ),
          undefined,
          dockerfileName,
        ],
        images.length > 0 && [
          "Inputs",
          <ul key="inputs">
            {images.map(({ uri, image }) => (
              <li key={uri}>{image}</li>
            ))}
          </ul>,
          undefined,
          images.map(({ image }) => image).join("\n"),
        ],
        sbom?.tooLarge && [
          "SBOM",
          <Fragment key="sbom">{formatBytes(sbom.size)}, too large to show here</Fragment>,
        ],
      ]}
    />
  );
}

function SourceLine({ source }: { source: NonNullable<ProvenanceSummary["source"]> }) {
  return (
    <>
      <ExternalOrText href={source.repositoryUrl}>{source.repository}</ExternalOrText>
      {source.subdirectory && (
        <span className="text-muted-foreground"> /{source.subdirectory}</span>
      )}
      {source.commit && (
        <>
          {" @ "}
          <ExternalOrText href={source.commitUrl}>{source.commit.slice(0, 12)}</ExternalOrText>
        </>
      )}
      {source.ref && source.ref !== source.commit && (
        <span className="text-muted-foreground"> ({source.ref})</span>
      )}
    </>
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

function ExternalOrText({ href, children }: { href?: string | undefined; children: ReactNode }) {
  if (!href) {
    return <>{children}</>;
  }

  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="inline-flex items-center gap-1 text-link hover:underline"
    >
      {children}
      <ExternalLink
        className="size-3 shrink-0"
        aria-hidden="true"
      />
    </a>
  );
}
