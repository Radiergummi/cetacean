import { Commit, ExternalAnchor } from "./DocoCDPanel";
import { IntegrationSection } from "./IntegrationSection";
import type { DeploymentActionIntegration } from "@/api/types";
import { KVTable } from "@/components/data";
import { formatYesNo } from "@/lib/format";

const docsUrl = "https://github.com/matchory/docker-swarm-deployment-action#deployment-labels";

function overrideTooltip(input: string): string {
  return `Overrides the action's ${input} input for this service, read from the Compose file on deploy`;
}

/**
 * Panel displaying the deployment provenance docker-swarm-deployment-action
 * writes onto a service, and the settings its labels override. The action
 * rewrites the former and reads the latter from the Compose file on every
 * deploy, so the panel has no form.
 */
export function DeploymentActionPanel({
  integration,
  rawLabels,
  serviceId,
  onSaved,
  editable,
}: {
  integration: DeploymentActionIntegration;
  rawLabels: [string, string][];
  serviceId: string;
  onSaved: (updated: Record<string, string>) => void;
  editable?: boolean | undefined;
}) {
  const {
    version,
    repository,
    repositoryUrl,
    ref,
    commit,
    commitUrl,
    runUrl,
    actor,
    monitor,
    monitorTimeoutSeconds,
    healthCheckWarnings,
  } = integration;

  return (
    <IntegrationSection
      title="GitHub Deployment"
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
            version && ["Version", version],
            repository && [
              "Repository",
              repositoryUrl ? (
                <ExternalAnchor
                  key="repository"
                  href={repositoryUrl}
                >
                  {repository}
                </ExternalAnchor>
              ) : (
                repository
              ),
              undefined,
              repository,
            ],
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
            runUrl && [
              "Workflow run",
              <ExternalAnchor
                key="run"
                href={runUrl}
              >
                {runUrl.slice(runUrl.lastIndexOf("/") + 1)}
              </ExternalAnchor>,
              undefined,
              runUrl,
            ],
            actor && ["Triggered by", actor],
          ]}
        />

        <KVTable
          rows={[
            ["Monitor", formatYesNo(monitor), overrideTooltip("monitor")],
            monitorTimeoutSeconds !== undefined && [
              "Monitor timeout",
              `${monitorTimeoutSeconds}s`,
              overrideTooltip("monitor-timeout"),
            ],
            [
              "Health check warnings",
              formatYesNo(healthCheckWarnings),
              overrideTooltip("health-check-warnings"),
            ],
          ]}
        />
      </div>
    </IntegrationSection>
  );
}
