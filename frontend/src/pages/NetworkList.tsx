import { api } from "../api/client";
import type { Network } from "../api/types";
import ResourceCard from "../components/ResourceCard";
import ResourceListPage from "../components/ResourceListPage";
import ResourceName from "../components/ResourceName";
import { sortColumn } from "../lib/sortColumn";

export default function NetworkList() {
  return (
    <ResourceListPage<Network>
      title="Networks"
      path="/networks"
      sseType="network"
      defaultSort="name"
      searchPlaceholder="Search networks…"
      viewModeKey="networks"
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
      ]}
      renderCard={({ Driver, Id, Name, Scope }) => (
        <ResourceCard
          title={<ResourceName name={Name} />}
          to={`/networks/${Id}`}
          meta={[Driver, Scope]}
        />
      )}
      emptyMessage={(hasSearch) =>
        hasSearch ? "No networks match your search" : "No networks found"
      }
    />
  );
}
