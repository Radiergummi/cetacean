import { api } from "../api/client";
import type { Network, Task } from "../api/types";
import AddressUse from "../components/AddressUse";
import ResourceCard from "../components/ResourceCard";
import ResourceListPage from "../components/ResourceListPage";
import ResourceName from "../components/ResourceName";
import { buttonVariants } from "../components/ui/button";
import type { SSEEvent } from "../hooks/useResourceStream";
import { sortColumn } from "../lib/sortColumn";
import { Link } from "react-router-dom";

/**
 * Whether an event can move a loaded row's address figure: a task only on a
 * listed network, a node only while ingress is listed. A task removed without
 * a payload might have been on one.
 */
export function movesAddresses({ type, resource }: SSEEvent, loaded: readonly Network[]): boolean {
  if (type === "node") {
    return loaded.some(({ Ingress }) => Ingress);
  }

  if (type !== "task" || !resource) {
    return true;
  }

  return ((resource as Task).NetworksAttachments ?? []).some(({ Network }) =>
    loaded.some(({ Id }) => Id === Network.ID),
  );
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
