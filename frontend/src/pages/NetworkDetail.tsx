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
import ResourceName from "../components/ResourceName";
import ServiceRefList from "../components/ServiceRefList";
import { useDetailResource } from "../hooks/useDetailResource";
import { parseStackLabels } from "../lib/parseStackLabels";
import { useParams } from "react-router-dom";

function NetworkFlags({ network }: { network: Network }) {
  const flags = [];

  if (network.Internal) {
    flags.push("Internal");
  }

  if (network.Attachable) {
    flags.push("Attachable");
  }

  if (network.Ingress) {
    flags.push("Ingress");
  }

  if (network.EnableIPv6) {
    flags.push("IPv6");
  }

  if (flags.length === 0) {
    return null;
  }

  return (
    <div className="flex flex-wrap gap-2">
      {flags.map((flag) => (
        <span
          key={flag}
          className="inline-flex items-center rounded-md bg-muted px-2.5 py-0.5 text-xs font-medium"
        >
          {flag}
        </span>
      ))}
    </div>
  );
}

function IPAMPanel({ network }: { network: Network }) {
  const ipam = network.IPAM;

  if (!ipam?.Config?.length) {
    return null;
  }

  return (
    <CollapsibleSection title="IPAM Configuration">
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {ipam.Config.map(({ Gateway, IPRange, Subnet }, index) => (
          <div
            key={index}
            className="rounded-lg border bg-card p-4"
          >
            <div className="space-y-2">
              {Subnet && (
                <div>
                  <div className="mb-0.5 text-xs font-medium tracking-wider text-muted-foreground uppercase">
                    Subnet
                  </div>
                  <div className="font-mono text-sm">{Subnet}</div>
                </div>
              )}
              {Gateway && (
                <div>
                  <div className="mb-0.5 text-xs font-medium tracking-wider text-muted-foreground uppercase">
                    Gateway
                  </div>
                  <div className="font-mono text-sm">{Gateway}</div>
                </div>
              )}
              {IPRange && (
                <div>
                  <div className="mb-0.5 text-xs font-medium tracking-wider text-muted-foreground uppercase">
                    IP Range
                  </div>
                  <div className="font-mono text-sm">{IPRange}</div>
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
  const { data, history, error, retry } = useDetailResource(id, api.network, `/networks/${id}`);

  if (error) {
    return (
      <FetchError
        message={error.message || "Failed to load network"}
        onRetry={retry}
      />
    );
  }
  if (!data) {
    return <LoadingDetail />;
  }

  const network = data.network;
  const services = data.services ?? [];
  const { entries: labelEntries, stack } = parseStackLabels(network.Labels);
  const options = Object.entries(network.Options || {});

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={
          <ResourceName
            name={network.Name}
            direction="column"
          />
        }
        breadcrumbs={[
          { label: "Networks", to: "/networks" },
          { label: <ResourceName name={network.Name} /> },
        ]}
      />

      <MetadataGrid>
        <ResourceId
          label="ID"
          id={network.Id}
        />
        <InfoCard
          label="Driver"
          value={network.Driver}
        />
        <InfoCard
          label="Scope"
          value={network.Scope}
        />
        <ResourceLink
          label="Stack"
          name={stack}
          to={`/stacks/${stack}`}
        />
        <Timestamp
          label="Created"
          date={network.Created}
        />
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
