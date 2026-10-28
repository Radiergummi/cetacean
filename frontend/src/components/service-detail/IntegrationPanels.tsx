import type { Integration } from "../../api/types";
import { rawLabelsForIntegration } from "../../lib/integrationLabels";
import { CaddyPanel } from "./CaddyPanel";
import { CronjobPanel } from "./CronjobPanel";
import { DeploymentActionPanel } from "./DeploymentActionPanel";
import { DiunPanel } from "./DiunPanel";
import { DocoCDPanel } from "./DocoCDPanel";
import { GantryPanel } from "./GantryPanel";
import { ShepherdPanel } from "./ShepherdPanel";
import { TraefikPanel } from "./TraefikPanel";

interface IntegrationPanelsProps {
  integrations: Integration[];
  serviceLabels: Record<string, string> | null;
  serviceId: string;
  onSaved: (labels: Record<string, string>) => void;
  editable: boolean;
}

export function IntegrationPanels({
  integrations,
  serviceLabels,
  serviceId,
  onSaved,
  editable,
}: IntegrationPanelsProps) {
  if (integrations.length === 0) {
    return null;
  }

  return (
    <>
      {integrations.map((integration) => {
        const rawLabels = rawLabelsForIntegration(serviceLabels, integration.labels);

        const panelProps = {
          rawLabels,
          serviceId,
          onSaved,
          editable,
        };

        switch (integration.name) {
          case "traefik":
            return (
              <TraefikPanel
                key={integration.name}
                integration={integration}
                {...panelProps}
              />
            );
          case "shepherd":
            return (
              <ShepherdPanel
                key={integration.name}
                integration={integration}
                {...panelProps}
              />
            );
          case "swarm-cronjob":
            return (
              <CronjobPanel
                key={integration.name}
                integration={integration}
                {...panelProps}
              />
            );
          case "diun":
            return (
              <DiunPanel
                key={integration.name}
                integration={integration}
                {...panelProps}
              />
            );
          case "caddy":
            return (
              <CaddyPanel
                key={integration.name}
                integration={integration}
                {...panelProps}
              />
            );
          case "doco-cd":
            return (
              <DocoCDPanel
                key={integration.name}
                integration={integration}
                {...panelProps}
              />
            );
          case "gantry":
            return (
              <GantryPanel
                key={integration.name}
                integration={integration}
                {...panelProps}
              />
            );
          case "docker-swarm-deployment-action":
            return (
              <DeploymentActionPanel
                key={integration.name}
                integration={integration}
                {...panelProps}
              />
            );
          default:
            return null;
        }
      })}
    </>
  );
}
