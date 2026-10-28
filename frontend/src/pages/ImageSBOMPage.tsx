import { ApiError, type AttestedKind, api, attestationPath } from "@/api/client";
import type { SBOMPackage } from "@/api/types";
import EmptyState from "@/components/EmptyState";
import FetchError from "@/components/FetchError";
import { LoadingDetail } from "@/components/LoadingSkeleton";
import PageHeader from "@/components/PageHeader";
import { SearchInput } from "@/components/search";
import SimpleTable from "@/components/SimpleTable";
import { buttonVariants } from "@/components/ui/button";
import { useAttestationBreadcrumbs } from "@/hooks/useAttestationBreadcrumbs";
import { apiPath } from "@/lib/basePath";
import { getErrorMessage } from "@/lib/utils";
import { useQuery } from "@tanstack/react-query";
import { useDeferredValue, useMemo, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";

const selectClassName =
  "flex min-w-0 rounded-md border bg-background px-3 py-2 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50";

const noLicense = "(none)";

function matches(entry: SBOMPackage, needle: string, ecosystem: string, license: string) {
  return (
    (!needle ||
      entry.name.toLowerCase().includes(needle) ||
      entry.purl.toLowerCase().includes(needle)) &&
    (ecosystem === "all" || entry.ecosystem === ecosystem) &&
    (license === "all" || (entry.license || noLicense) === license)
  );
}

/**
 * The packages of one platform of an image, from the SPDX SBOM BuildKit
 * attached to it. The SBOM is what the image says it contains; nothing here
 * checks it.
 */
export default function ImageSBOMPage({ kind }: { kind: AttestedKind }) {
  const { id = "" } = useParams<{ id: string }>();
  const [searchParams] = useSearchParams();
  const platform = searchParams.get("platform") ?? undefined;

  const [query, setQuery] = useState("");
  const [ecosystem, setEcosystem] = useState("all");
  const [license, setLicense] = useState("all");

  const { data, error, isLoading, refetch } = useQuery({
    queryKey: ["sbom", kind, id, platform],
    queryFn: ({ signal }) => api.sbom(kind, id, platform, signal),
  });

  const platformMissing =
    error instanceof ApiError && (error.code === "ATT003" || error.code === "ATT004");

  const packages = useMemo(() => data?.packages ?? [], [data]);
  const licenses = useMemo(
    () => [...new Set(packages.map(({ license: value }) => value || noLicense))].sort(),
    [packages],
  );
  const ecosystems = useMemo(() => Object.keys(data?.summary.ecosystems ?? {}).sort(), [data]);

  const deferredQuery = useDeferredValue(query);
  const needle = deferredQuery.trim().toLowerCase();
  const filtered = useMemo(
    () => packages.filter((entry) => matches(entry, needle, ecosystem, license)),
    [packages, needle, ecosystem, license],
  );

  if (platformMissing) {
    return (
      <Page
        kind={kind}
        id={id}
        subtitle="This image has more than one platform; pick one."
      >
        <PlatformPicker
          kind={kind}
          id={id}
        />
      </Page>
    );
  }

  if (error) {
    return (
      <Page
        kind={kind}
        id={id}
      >
        <FetchError
          message={getErrorMessage(error, "Failed to load the SBOM")}
          onRetry={() => refetch()}
        />
      </Page>
    );
  }

  if (isLoading || !data) {
    return (
      <Page
        kind={kind}
        id={id}
      >
        <LoadingDetail />
      </Page>
    );
  }

  const onlyGo = ecosystems.length === 1 && ecosystems[0] === "golang";

  return (
    <Page
      kind={kind}
      id={id}
      subtitle={`${data.summary.total} packages on ${data.platform}, ${data.summary.licensed} with a license, as the image's SBOM lists them.`}
      actions={
        <div className="flex gap-2">
          <a
            href={apiPath(attestationPath(kind, id, "sbom", data.platform, ".csv"))}
            className={buttonVariants({ variant: "outline", size: "sm" })}
          >
            Download CSV
          </a>
          <a
            href={apiPath(attestationPath(kind, id, "sbom", data.platform, ".spdx"))}
            className={buttonVariants({ variant: "outline", size: "sm" })}
          >
            Download SPDX
          </a>
        </div>
      }
    >
      {data.summary.licensed === 0 && onlyGo && (
        <p className="mb-4 text-sm text-muted-foreground">
          Go modules carry no license in Syft's SBOMs, so the license column is empty.
        </p>
      )}

      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-stretch">
        <SearchInput
          value={query}
          onChange={setQuery}
          placeholder="Search by name or package URL…"
          className="sm:w-72 sm:shrink-0"
        />
        <select
          aria-label="Ecosystem"
          value={ecosystem}
          onChange={(event) => setEcosystem(event.target.value)}
          className={selectClassName}
        >
          <option value="all">All ecosystems</option>
          {ecosystems.map((value) => (
            <option
              key={value}
              value={value}
            >
              {value} ({data.summary.ecosystems?.[value] ?? 0})
            </option>
          ))}
        </select>
        <select
          aria-label="License"
          value={license}
          onChange={(event) => setLicense(event.target.value)}
          className={`${selectClassName} sm:max-w-64`}
        >
          <option value="all">All licenses</option>
          {licenses.map((value) => (
            <option
              key={value}
              value={value}
            >
              {value}
            </option>
          ))}
        </select>
      </div>

      <SimpleTable
        columns={["Name", "Version", "Ecosystem", "License", "Found in"]}
        items={filtered}
        keyFn={(entry) => `${entry.purl}|${entry.name}|${entry.version}|${entry.foundIn}`}
        renderRow={(entry) => (
          <>
            <td
              className="p-3 text-sm font-medium"
              title={entry.purl}
            >
              {entry.name}
            </td>
            <td className="p-3 text-sm">{entry.version}</td>
            <td className="p-3 text-sm">{entry.ecosystem}</td>
            <td className="p-3 text-sm">
              <div className="max-w-64 break-words whitespace-normal">{entry.license}</div>
            </td>
            <td className="p-3 font-mono text-xs text-muted-foreground">
              <div className="max-w-96 break-all whitespace-normal">{entry.foundIn}</div>
            </td>
          </>
        )}
      />

      {filtered.length === 0 && <EmptyState message="No matching packages." />}
    </Page>
  );
}

function Page({
  kind,
  id,
  subtitle,
  actions,
  children,
}: {
  kind: AttestedKind;
  id: string;
  subtitle?: string | undefined;
  actions?: React.ReactNode | undefined;
  children: React.ReactNode;
}) {
  const breadcrumbs = useAttestationBreadcrumbs(kind, id, "SBOM");

  return (
    <>
      <PageHeader
        title="SBOM"
        subtitle={subtitle}
        actions={actions}
        breadcrumbs={breadcrumbs}
      />
      {children}
    </>
  );
}

function PlatformPicker({ kind, id }: { kind: AttestedKind; id: string }) {
  const { data } = useQuery({
    queryKey: ["provenance", kind, id],
    queryFn: ({ signal }) => api.provenance(kind, id, signal),
  });

  return (
    <ul className="flex flex-wrap gap-2">
      {(data?.platforms ?? [])
        .filter(({ sbom }) => sbom === undefined || !sbom.tooLarge)
        .map(({ platform }) => (
          <li key={platform}>
            <Link
              to={attestationPath(kind, id, "sbom", platform)}
              className={buttonVariants({ variant: "outline", size: "sm" })}
            >
              {platform}
            </Link>
          </li>
        ))}
    </ul>
  );
}
