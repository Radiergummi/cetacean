import { IntegrationSection } from "./IntegrationSection";
import type { DocoCDIntegration } from "@/api/types";
import { KVTable } from "@/components/data";
import TimeAgo from "@/components/TimeAgo";
import { formatDateTime, formatYesNo } from "@/lib/format";
import { ExternalLink } from "lucide-react";

const docsUrl = "https://github.com/kimdre/doco-cd/wiki";

const fullCommitSHA = /^[0-9a-f]{40,64}$/i;

function shortRevision(value: string): string {
  return fullCommitSHA.test(value) ? value.slice(0, 7) : value;
}

export function Commit({ commit, commitUrl }: { commit: string; commitUrl?: string | undefined }) {
  const short = <code title={commit}>{shortRevision(commit)}</code>;

  if (!commitUrl) {
    return short;
  }

  return (
    <a
      href={commitUrl}
      target="_blank"
      rel="noopener noreferrer"
      className="inline-flex items-center gap-1 hover:underline"
    >
      {short}
      <ExternalLink className="size-3" />
    </a>
  );
}

/**
 * Panel displaying the deployment provenance doco-cd writes onto a service.
 * doco-cd rewrites these labels on every deploy, so the panel has no form.
 */
export function DocoCDPanel({
  integration,
  rawLabels,
  serviceId,
  onSaved,
  editable,
}: {
  integration: DocoCDIntegration;
  rawLabels: [string, string][];
  serviceId: string;
  onSaved: (updated: Record<string, string>) => void;
  editable?: boolean | undefined;
}) {
  const {
    deployment,
    source,
    repository,
    sourceUrl,
    ref,
    commit,
    commitUrl,
    trigger,
    deployedAt,
    version,
    certExpiry,
    job,
  } = integration;

  return (
    <IntegrationSection
      title="doco-cd"
      defaultOpen
      enabled
      rawLabels={rawLabels}
      docsUrl={docsUrl}
      editable={editable}
      serviceId={serviceId}
      onRawSave={onSaved}
    >
      <div className="flex flex-col gap-3">
        <KVTable
          rows={[
            deployment && ["Deployment", deployment],
            repository && ["Repository", repository],
            sourceUrl && ["Source URL", sourceUrl],
            ref && ["Ref", ref],
            commit && [
              "Commit",
              <Commit
                key="commit"
                commit={commit}
                commitUrl={commitUrl}
              />,
              undefined,
              commit,
            ],
            deployedAt && [
              "Deployed",
              <TimeAgo
                key="deployed"
                date={deployedAt}
              />,
            ],
            trigger && trigger !== commit && ["Trigger", shortRevision(trigger)],
            source && ["Source type", source],
            certExpiry && ["Certificate expires", formatDateTime(certExpiry)],
            version && ["doco-cd version", version],
          ]}
        />

        {job && (
          <KVTable
            rows={[
              ["Scheduled job", job.enabled ? "Enabled" : "Disabled"],
              job.schedule && ["Schedule", job.schedule],
              job.executionMode && ["Execution mode", job.executionMode],
              ["Skip running", formatYesNo(job.skipRunning)],
              job.lastRun && [
                "Last run",
                <TimeAgo
                  key="last-run"
                  date={job.lastRun}
                />,
              ],
              job.nextRun && ["Next run", formatDateTime(job.nextRun)],
            ]}
          />
        )}
      </div>
    </IntegrationSection>
  );
}
