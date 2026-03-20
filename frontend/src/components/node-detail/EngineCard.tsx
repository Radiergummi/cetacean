import { api } from "@/api/client";
import InfoCard from "@/components/InfoCard";
import { ArrowUpRight } from "lucide-react";
import { useEffect, useState } from "react";

function isNewer(latest: string, current: string): boolean {
  const parse = (version: string) => version.split(".").map(Number);
  const latestParts = parse(latest);
  const currentParts = parse(current);

  for (let index = 0; index < Math.max(latestParts.length, currentParts.length); index++) {
    const latestPart = latestParts[index] ?? 0;
    const currentPart = currentParts[index] ?? 0;

    if (latestPart > currentPart) {
      return true;
    }

    if (latestPart < currentPart) {
      return false;
    }
  }

  return false;
}

export function EngineCard({ version }: { version: string }) {
  const [latest, setLatest] = useState<{ version: string; url: string } | null>(null);

  useEffect(() => {
    api
      .dockerLatestVersion()
      .then(setLatest)
      .catch(() => {});
  }, []);

  const updateAvailable = latest && isNewer(latest.version, version);

  return (
    <InfoCard
      label="Engine"
      value={
        <>
          {version}
          {updateAvailable && (
            <a
              href={latest.url}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-0.5 rounded bg-amber-500/10 px-1.5 py-0.5 text-xs font-medium text-amber-600 hover:bg-amber-500/20 dark:text-amber-400"
            >
              {latest.version} available
              <ArrowUpRight className="size-3" />
            </a>
          )}
        </>
      }
    />
  );
}
