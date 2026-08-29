import { api } from "@/api/client";
import type { LicenseComponent } from "@/api/types";
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

  const counts = useMemo(() => {
    const result: Record<string, number> = { all: components.length };

    for (const component of components) {
      result[component.ecosystem] = (result[component.ecosystem] ?? 0) + 1;
    }

    return result;
  }, [components]);

  const licenseOptions = useMemo(() => {
    const counts = new Map<string, number>();

    for (const component of components) {
      for (const entry of component.licenses) {
        const id = entry.id || entry.name || "Unknown";

        counts.set(id, (counts.get(id) ?? 0) + 1);
      }
    }

    const sorted = [...counts.entries()].sort(([a], [b]) => a.localeCompare(b));

    return [
      { value: "all", label: "All licenses" },
      ...sorted.map(([id, count]) => ({ value: id, label: `${id} (${count})` })),
    ];
  }, [components]);

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();

    return components
      .filter((component) => {
        if (ecosystem !== "all" && component.ecosystem !== ecosystem) {
          return false;
        }

        if (
          license !== "all" &&
          !component.licenses.some((entry) => (entry.id || entry.name || "Unknown") === license)
        ) {
          return false;
        }

        if (needle && !component.name.toLowerCase().includes(needle)) {
          return false;
        }

        return true;
      })
      .sort((a, b) => a.name.localeCompare(b.name));
  }, [components, query, ecosystem, license]);

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

  if (counts.other) {
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

        <div className="flex flex-wrap items-center gap-2">
          <Combobox
            value={license}
            onChange={setLicense}
            options={licenseOptions}
            allowCustom={false}
            className="sm:max-w-56"
          />

          {ecosystems.map((value) => (
            <button
              key={value}
              type="button"
              onClick={() => setEcosystem(value)}
              className={cn(
                "rounded-md px-3 py-1 text-xs",
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
        {component.licenses.map((license, index) =>
          component.textId ? (
            <button
              key={license.id || license.name || String(index)}
              type="button"
              onClick={() => setShowText(true)}
              className="cursor-pointer"
            >
              <Badge variant="secondary">{license.id || license.name || "Unknown"}</Badge>
            </button>
          ) : (
            <Badge
              key={license.id || license.name || String(index)}
              variant="secondary"
            >
              {license.id || license.name || "Unknown"}
            </Badge>
          ),
        )}

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
