import { api } from "../api/client";
import type { Network, NetworkAddresses, Task } from "../api/types";
import ResourceCard from "../components/ResourceCard";
import ResourceListPage from "../components/ResourceListPage";
import ResourceName from "../components/ResourceName";
import { buttonVariants } from "../components/ui/button";
import type { SSEEvent } from "../hooks/useResourceStream";
import { addressTone } from "../lib/networkAddresses";
import { sortColumn } from "../lib/sortColumn";
import { toneText } from "../lib/statusColor";
import { Link } from "react-router-dom";

function AddressUse({ addresses }: { addresses: NetworkAddresses }) {
  const tone = addressTone(addresses);

  return (
    <span className={`tabular-nums ${tone ? toneText[tone] : ""}`}>
      {addresses.used} / {addresses.usable}
    </span>
  );
}

/**
 * A network's address figure moves with its own changes, the VIPs services
 * hold, the nodes in the swarm, and the tasks attached to it; any other task
 * event, which a busy cluster sends constantly, would refetch for nothing. A
 * task removed without a payload might have been attached.
 */
export function movesAddresses({ type, resource }: SSEEvent): boolean {
  if (type !== "task" || !resource) {
    return true;
  }

  return ((resource as Task).NetworksAttachments ?? []).length > 0;
}

export default function NetworkList() {
  return (
    <ResourceListPage<Network>
      title="Networks"
      path="/networks"
      sseType="network,task,service,node"
      derived
      relevantEvent={movesAddresses}
      defaultSort="name"
      searchPlaceholder="Search networks…"
      viewModeKey="networks"
      actions={() => (
        <Link
          to="/ports"
          className={buttonVariants({ variant: "outline" })}
        >
          Published ports
        </Link>
      )}
      fetchFn={(params, signal) => api.networks(params, signal)}
      keyFn={({ Id }) => Id}
      itemPath={({ Id }) => `/networks/${Id}`}
      columns={(sortKey, sortDir, toggle) => [
        {
          ...sortColumn("Name", "name", sortKey, sortDir, toggle),
          cell: ({ Name }) => <ResourceName name={Name} />,
        },
        {
          ...sortColumn("Driver", "driver", sortKey, sortDir, toggle),
          cell: ({ Driver }) => Driver,
        },
        {
          ...sortColumn("Scope", "scope", sortKey, sortDir, toggle),
          cell: ({ Scope }) => Scope,
        },
        {
          ...sortColumn("Addresses", "addresses", sortKey, sortDir, toggle),
          cell: ({ Addresses }) => (Addresses ? <AddressUse addresses={Addresses} /> : "\u2014"),
        },
      ]}
      renderCard={({ Addresses, Driver, Id, Name, Scope }) => (
        <ResourceCard
          title={<ResourceName name={Name} />}
          to={`/networks/${Id}`}
          meta={[
            Driver,
            Scope,
            ...(Addresses
              ? [
                  <span key="addresses">
                    <AddressUse addresses={Addresses} /> addresses
                  </span>,
                ]
              : []),
          ]}
        />
      )}
      emptyMessage={(hasSearch) =>
        hasSearch ? "No networks match your search" : "No networks found"
      }
    />
  );
}
