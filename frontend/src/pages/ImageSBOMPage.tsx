import { ApiError, type AttestedKind, api, attestationPath } from "@/api/client";
import type { SBOMPackage } from "@/api/types";
import EmptyState from "@/components/EmptyState";
import FetchError from "@/components/FetchError";
import { LoadingDetail } from "@/components/LoadingSkeleton";
import PageHeader from "@/components/PageHeader";
import { SearchInput } from "@/components/search";
import { buttonVariants } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { apiPath } from "@/lib/basePath";
import { getErrorMessage } from "@/lib/utils";
import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";

const selectClassName =
  "flex h-8 rounded-md border border-input bg-transparent px-3 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50";

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

  const needle = query.trim().toLowerCase();
  const filtered = useMemo(
    () => packages.filter((entry) => matches(entry, needle, ecosystem, license)),
    [packages, needle, ecosystem, license],
  );

  const back = `/${kind}s/${id}`;

  if (platformMissing) {
    return (
      <Page
        back={back}
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
      <Page back={back}>
        <FetchError
          message={getErrorMessage(error, "Failed to load the SBOM")}
          onRetry={() => refetch()}
        />
      </Page>
    );
  }

  if (isLoading || !data) {
    return (
      <Page back={back}>
        <LoadingDetail />
      </Page>
    );
  }

  const onlyGo = ecosystems.length === 1 && ecosystems[0] === "golang";

  return (
    <Page
      back={back}
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

      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center">
        <SearchInput
          value={query}
          onChange={setQuery}
          placeholder="Search by name or package URL…"
          className="sm:max-w-xs"
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
          className={selectClassName}
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

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Version</TableHead>
            <TableHead>Ecosystem</TableHead>
            <TableHead>License</TableHead>
            <TableHead>Found in</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {filtered.map((entry) => (
            <TableRow key={`${entry.purl}|${entry.name}|${entry.version}|${entry.foundIn}`}>
              <TableCell
                className="font-medium break-all"
                title={entry.purl}
              >
                {entry.name}
              </TableCell>
              <TableCell className="break-all">{entry.version}</TableCell>
              <TableCell>{entry.ecosystem}</TableCell>
              <TableCell className="break-words">{entry.license}</TableCell>
              <TableCell className="break-all text-muted-foreground">{entry.foundIn}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      {filtered.length === 0 && <EmptyState message="No matching packages." />}
    </Page>
  );
}

function Page({
  back,
  subtitle,
  actions,
  children,
}: {
  back: string;
  subtitle?: string | undefined;
  actions?: React.ReactNode | undefined;
  children: React.ReactNode;
}) {
  return (
    <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
      <PageHeader
        title="SBOM"
        subtitle={subtitle}
        actions={actions}
        breadcrumbs={[
          { label: back.startsWith("/services") ? "Service" : "Task", to: back },
          { label: "SBOM" },
        ]}
      />
      {children}
    </div>
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
