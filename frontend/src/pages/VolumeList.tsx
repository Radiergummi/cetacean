import { api } from "../api/client";
import type { Volume } from "../api/types";
import ResourceCard from "../components/ResourceCard";
import ResourceListPage from "../components/ResourceListPage";
import ResourceName from "../components/ResourceName";
import { sortColumn } from "../lib/sortColumn";

export default function VolumeList() {
  return (
    <ResourceListPage<Volume>
      title="Volumes"
      path="/volumes"
      sseType="volume"
      defaultSort="name"
      searchPlaceholder="Search volumes…"
      viewModeKey="volumes"
      fetchFn={(params, signal) => api.volumes(params, signal)}
      keyFn={({ Name }) => Name}
      itemPath={({ Name }) => `/volumes/${Name}`}
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
      renderCard={({ Driver, Name, Scope }) => (
        <ResourceCard
          title={<ResourceName name={Name} />}
          to={`/volumes/${Name}`}
          meta={[Driver, Scope]}
        />
      )}
      emptyMessage={(hasSearch) =>
        hasSearch ? "No volumes match your search" : "No volumes found"
      }
    />
  );
}
