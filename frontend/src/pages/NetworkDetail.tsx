import { useParams } from "react-router-dom";
import { api } from "../api/client";
import type { Network } from "../api/types";
import ActivitySection from "../components/ActivitySection";
import CollapsibleSection from "../components/CollapsibleSection";
import {
  KeyValuePills,
  LabelSection,
  MetadataGrid,
  ResourceId,
  ResourceLink,
  Timestamp,
} from "../components/data";
import FetchError from "../components/FetchError";
import InfoCard from "../components/InfoCard";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import ServiceRefList from "../components/ServiceRefList";
import { useDetailResource } from "../hooks/useDetailResource";
import { parseStackLabels } from "../lib/parseStackLabels";

function NetworkFlags({ network }: { network: Network }) {
  const flags = [];
  if (network.Internal) flags.push("Internal");
  if (network.Attachable) flags.push("Attachable");
  if (network.Ingress) flags.push("Ingress");
  if (network.EnableIPv6) flags.push("IPv6");
  if (flags.length === 0) return null;
  return (
    <div className="flex flex-wrap gap-2">
      {flags.map((f) => (
        <span
          key={f}
          className="inline-flex items-center rounded-md bg-muted px-2.5 py-0.5 text-xs font-medium"
        >
          {f}
        </span>
      ))}
    </div>
  );
}

function IPAMPanel({ network }: { network: Network }) {
  const ipam = network.IPAM;
  if (!ipam?.Config?.length) return null;

  return (
    <CollapsibleSection title="IPAM Configuration">
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        {ipam.Config.map((cfg, i) => (
          <div key={i} className="rounded-lg border bg-card p-4">
            <div className="space-y-2">
              {cfg.Subnet && (
                <div>
                  <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground mb-0.5">
                    Subnet
                  </div>
                  <div className="text-sm font-mono">{cfg.Subnet}</div>
                </div>
              )}
              {cfg.Gateway && (
                <div>
                  <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground mb-0.5">
                    Gateway
                  </div>
                  <div className="text-sm font-mono">{cfg.Gateway}</div>
                </div>
              )}
              {cfg.IPRange && (
                <div>
                  <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground mb-0.5">
                    IP Range
                  </div>
                  <div className="text-sm font-mono">{cfg.IPRange}</div>
                </div>
              )}
            </div>
          </div>
        ))}
      </div>
      {ipam.Driver && ipam.Driver !== "default" && (
        <div className="mt-2 text-xs text-muted-foreground">
          IPAM Driver: <span className="font-mono">{ipam.Driver}</span>
        </div>
      )}
    </CollapsibleSection>
  );
}

export default function NetworkDetail() {
  const { id } = useParams<{ id: string }>();
  const { data, history, error } = useDetailResource(id, api.network, `/networks/${id}`);

  if (error) return <FetchError message="Failed to load network" />;
  if (!data) return <LoadingDetail />;

  const network = data.network;
  const services = data.services ?? [];
  const { entries: labelEntries, stack } = parseStackLabels(network.Labels);
  const options = Object.entries(network.Options || {});

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={network.Name}
        breadcrumbs={[{ label: "Networks", to: "/networks" }, { label: network.Name }]}
      />

      <MetadataGrid>
        <ResourceId label="ID" id={network.Id} />
        <InfoCard label="Driver" value={network.Driver} />
        <InfoCard label="Scope" value={network.Scope} />
        <ResourceLink label="Stack" name={stack} to={`/stacks/${stack}`} />
        <Timestamp label="Created" date={network.Created} />
      </MetadataGrid>

      <NetworkFlags network={network} />

      <IPAMPanel network={network} />

      {options.length > 0 && (
        <CollapsibleSection title="Driver Options">
          <KeyValuePills entries={options} />
        </CollapsibleSection>
      )}

      <LabelSection entries={labelEntries} />

      <ServiceRefList
        services={services}
        label="Connected Services"
        emptyMessage="No services using this network."
      />

      <ActivitySection entries={history} />
    </div>
  );
}
