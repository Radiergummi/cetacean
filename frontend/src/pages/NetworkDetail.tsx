import { useParams } from "react-router-dom";
import { useState, useEffect, useCallback } from "react";
import { api } from "../api/client";
import type { Network, ServiceRef, HistoryEntry } from "../api/types";
import InfoCard from "../components/InfoCard";
import PageHeader from "../components/PageHeader";
import { LoadingDetail } from "../components/LoadingSkeleton";
import FetchError from "../components/FetchError";
import ActivityFeed from "../components/ActivityFeed";
import ServiceRefList from "../components/ServiceRefList";
import { useSSE } from "../hooks/useSSE";
import { KeyValuePills, ResourceId, ResourceLink, Timestamp } from "../components/data";

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
    <div>
      <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
        IPAM Configuration
      </h2>
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
    </div>
  );
}

export default function NetworkDetail() {
  const { id } = useParams<{ id: string }>();
  const [network, setNetwork] = useState<Network | null>(null);
  const [services, setServices] = useState<ServiceRef[]>([]);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [error, setError] = useState(false);

  const fetchData = useCallback(() => {
    if (!id) return;
    api
      .network(id)
      .then((d) => {
        setNetwork(d.network);
        setServices(d.services ?? []);
      })
      .catch(() => setError(true));
    api
      .history({ resourceId: id, limit: 10 })
      .then(setHistory)
      .catch(() => {});
  }, [id]);

  useEffect(fetchData, [fetchData]);

  useSSE(["network", "service"], (e) => {
    if (e.type === "network" && e.id === id) fetchData();
    if (e.type === "service") fetchData();
  });

  if (error) return <FetchError message="Failed to load network" />;
  if (!network) return <LoadingDetail />;

  const labels = network.Labels || {};
  const labelEntries = Object.entries(labels).filter(([k]) => k !== "com.docker.stack.namespace");
  const stack = labels["com.docker.stack.namespace"];
  const options = Object.entries(network.Options || {});

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={network.Name}
        breadcrumbs={[{ label: "Networks", to: "/networks" }, { label: network.Name }]}
      />

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        <ResourceId label="ID" id={network.Id} />
        <InfoCard label="Driver" value={network.Driver} />
        <InfoCard label="Scope" value={network.Scope} />
        <ResourceLink label="Stack" name={stack} to={`/stacks/${stack}`} />
        <Timestamp label="Created" date={network.Created} />
      </div>

      <NetworkFlags network={network} />

      <IPAMPanel network={network} />

      {options.length > 0 && (
        <div>
          <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
            Driver Options
          </h2>
          <KeyValuePills entries={options} />
        </div>
      )}

      {labelEntries.length > 0 && (
        <div>
          <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
            Labels
          </h2>
          <KeyValuePills entries={labelEntries} />
        </div>
      )}

      <ServiceRefList
        services={services}
        label="Connected Services"
        emptyMessage="No services using this network."
      />

      {history.length > 0 && (
        <div>
          <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
            Recent Activity
          </h2>
          <ActivityFeed entries={history} />
        </div>
      )}
    </div>
  );
}
