import { api } from "@/api/client";
import type { LicenseComponent, LicenseEntry } from "@/api/types";
import EmptyState from "@/components/EmptyState";
import FetchError from "@/components/FetchError";
import LicenseTextDialog from "@/components/LicenseTextDialog";
import { LoadingDetail } from "@/components/LoadingSkeleton";
import PageHeader from "@/components/PageHeader";
import { SearchInput } from "@/components/search";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Combobox } from "@/components/ui/combobox";
import { apiPath } from "@/lib/basePath";
import { cn, getErrorMessage } from "@/lib/utils";
import { useQuery } from "@tanstack/react-query";
import { ExternalLink } from "lucide-react";
import { useMemo, useState } from "react";

type EcosystemFilter = "all" | "go" | "npm" | "other";

const ecosystemLabels: Record<string, string> = {
  go: "Go",
  npm: "npm",
  other: "Other",
};

/**
 * The label a license is known by everywhere on this page. The filter matches
 * on it, the dropdown counts it, and the badge shows it — they have to agree
 * exactly or the filter silently stops matching.
 */
function licenseLabel({ id, name }: LicenseEntry): string {
  return id || name || "Unknown";
}

function matchesSearch(component: LicenseComponent, needle: string): boolean {
  return !needle || component.name.toLowerCase().includes(needle);
}

function matchesEcosystem(component: LicenseComponent, ecosystem: EcosystemFilter): boolean {
  return ecosystem === "all" || component.ecosystem === ecosystem;
}

function matchesLicense(component: LicenseComponent, license: string): boolean {
  return license === "all" || component.licenses.some((entry) => licenseLabel(entry) === license);
}

/** The distinct licenses a component carries, so it counts once per license. */
function licenseLabelsOf(component: LicenseComponent): Set<string> {
  return new Set(component.licenses.map(licenseLabel));
}

/**
 * Applies basic heuristics to try and convert VCS URLs targeting git to HTTP
 * (e.g., "git://…" or "git+https://…"), so the link opens in a browser. SBOM
 * URLs are untrusted input, so anything that does not normalize to http(s)
 * (e.g. a "javascript:" scheme) returns null and is not rendered as a link.
 */
function browserUrl(url: string): string | null {
  const normalized = url.replace(/^git(\+https)?:\/\//, "https://").replace(/\.git$/, "");

  return /^https?:\/\//i.test(normalized) ? normalized : null;
}

export default function Licenses() {
  const [query, setQuery] = useState("");
  const [ecosystem, setEcosystem] = useState<EcosystemFilter>("all");
  const [license, setLicense] = useState("all");

  const {
    data,
    error: queryError,
    isLoading,
    refetch,
  } = useQuery({
    queryKey: ["licenses"],
    queryFn: ({ signal }) => api.licenses(signal),
  });

  const error = queryError ? getErrorMessage(queryError, "Failed to load licenses") : null;
  const components = useMemo(() => data?.components ?? [], [data]);

  const needle = query.trim().toLowerCase();

  // Each control counts against every filter except its own, so it always
  // reports what picking it would leave rather than counting itself out.
  const licenseAndSearchMatches = useMemo(
    () =>
      components.filter(
        (component) => matchesLicense(component, license) && matchesSearch(component, needle),
      ),
    [components, license, needle],
  );

  const ecosystemAndSearchMatches = useMemo(
    () =>
      components.filter(
        (component) => matchesEcosystem(component, ecosystem) && matchesSearch(component, needle),
      ),
    [components, ecosystem, needle],
  );

  const counts = useMemo(() => {
    const result: Record<string, number> = { all: licenseAndSearchMatches.length };

    for (const component of licenseAndSearchMatches) {
      result[component.ecosystem] = (result[component.ecosystem] ?? 0) + 1;
    }

    return result;
  }, [licenseAndSearchMatches]);

  // Which chips exist is a property of the inventory, not of the current
  // filter: a chip that vanished as you typed would take its own selection
  // with it.
  const hasOtherEcosystem = useMemo(
    () => components.some((component) => component.ecosystem === "other"),
    [components],
  );

  const licenseOptions = useMemo(() => {
    // Counting only what is in scope is also what hides the empty options: a
    // license nothing here carries never reaches the map, and an option that
    // would return nothing is not worth offering. "All licenses" always
    // remains, so a filter that narrows to nothing is still one click to undo.
    const counts = new Map<string, number>();

    for (const component of ecosystemAndSearchMatches) {
      for (const label of licenseLabelsOf(component)) {
        counts.set(label, (counts.get(label) ?? 0) + 1);
      }
    }

    const sorted = [...counts.entries()].sort(([a], [b]) => a.localeCompare(b));

    return [
      { value: "all", label: "All licenses", trailing: ecosystemAndSearchMatches.length },
      ...sorted.map(([label, count]) => ({ value: label, label, trailing: count })),
    ];
  }, [ecosystemAndSearchMatches]);

  const filtered = useMemo(
    () =>
      licenseAndSearchMatches
        .filter((component) => matchesEcosystem(component, ecosystem))
        .sort((a, b) => a.name.localeCompare(b.name)),
    [licenseAndSearchMatches, ecosystem],
  );

  if (error) {
    return (
      <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
        <FetchError
          message={error}
          onRetry={() => refetch()}
        />
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
        <LoadingDetail />
      </div>
    );
  }

  const ecosystems: EcosystemFilter[] = ["all", "go", "npm"];

  if (hasOtherEcosystem) {
    ecosystems.push("other");
  }

  return (
    <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
      <PageHeader
        title="Open-source licenses"
        subtitle="Every open-source project bundled into Cetacean."
        actions={
          <a
            href={apiPath("/-/notices")}
            download="THIRD_PARTY_LICENSES.txt"
            className={buttonVariants({ variant: "outline", size: "sm" })}
          >
            Download all notices
          </a>
        }
      />

      <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <SearchInput
          value={query}
          onChange={setQuery}
          placeholder="Search by name…"
          className="sm:max-w-xs"
        />

        {/* One row: the chips never wrap under the dropdown. On a viewport too
            narrow to hold them all, the row scrolls rather than reflowing. */}
        <div className="flex items-center gap-2 overflow-x-auto">
          <Combobox
            value={license}
            onChange={setLicense}
            options={licenseOptions}
            allowCustom={false}
            className="h-7 w-44 shrink-0 text-xs"
          />

          {ecosystems.map((value) => (
            <button
              key={value}
              type="button"
              onClick={() => setEcosystem(value)}
              className={cn(
                "inline-flex h-7 shrink-0 items-center rounded-md px-3 text-xs",
                value === ecosystem
                  ? "bg-primary font-medium text-primary-foreground"
                  : "border text-muted-foreground transition hover:text-foreground",
              )}
            >
              {value === "all" ? "All" : ecosystemLabels[value]}
              <span className="ml-1.5 opacity-60">{counts[value] ?? 0}</span>
            </button>
          ))}
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {filtered.map((component) => (
          <LicenseCard
            key={`${component.ecosystem}:${component.name}@${component.version ?? ""}`}
            component={component}
          />
        ))}
      </div>

      {data && filtered.length === 0 && <EmptyState message="No matching components." />}
    </div>
  );
}

function LicenseCard({ component }: { component: LicenseComponent }) {
  const [showText, setShowText] = useState(false);

  const homepageUrl = component.homepage ? browserUrl(component.homepage) : null;
  const repositoryUrl = component.repository ? browserUrl(component.repository) : null;

  // Every license on the card opens the same text, so the badges become
  // buttons only when there is a text to open.
  const openTextProps = component.textId
    ? {
        render: <button type="button" />,
        onClick: () => setShowText(true),
        className: "cursor-pointer",
      }
    : {};

  return (
    <Card className="flex flex-col gap-2 p-4">
      <div className="flex items-start justify-between gap-2">
        {homepageUrl ? (
          <a
            href={homepageUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="truncate font-medium transition hover:text-primary"
          >
            {component.name}
          </a>
        ) : (
          <span className="truncate font-medium">{component.name}</span>
        )}

        <Badge variant="outline">
          {ecosystemLabels[component.ecosystem] ?? component.ecosystem}
        </Badge>
      </div>

      {component.version && (
        <span className="font-mono text-xs text-muted-foreground">{component.version}</span>
      )}

      {component.description && (
        <p className="line-clamp-2 text-xs text-muted-foreground">{component.description}</p>
      )}

      <div className="mt-auto flex flex-wrap items-center gap-1.5 pt-1">
        {component.licenses.map((license, index) => (
          <Badge
            key={license.id || license.name || String(index)}
            variant="secondary"
            {...openTextProps}
          >
            {licenseLabel(license)}
          </Badge>
        ))}

        {repositoryUrl && (
          <a
            href={repositoryUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="ml-auto inline-flex items-center gap-1 text-xs text-muted-foreground transition hover:text-foreground"
          >
            <ExternalLink className="size-3" />
            Source
          </a>
        )}
      </div>

      {component.textId && (
        <LicenseTextDialog
          component={component}
          open={showText}
          onOpenChange={setShowText}
        />
      )}
    </Card>
  );
}
