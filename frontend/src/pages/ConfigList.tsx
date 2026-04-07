import { api } from "../api/client";
import type { Config } from "../api/types";
import CreateDataResourceForm from "../components/CreateDataResourceForm";
import ResourceCard from "../components/ResourceCard";
import ResourceListPage from "../components/ResourceListPage";
import ResourceName from "../components/ResourceName";
import TimeAgo from "../components/TimeAgo";
import { sortColumn } from "../lib/sortColumn";

export default function ConfigList() {
  return (
    <ResourceListPage<Config>
      title="Configs"
      path="/configs"
      sseType="config"
      defaultSort="name"
      searchPlaceholder="Search configs…"
      viewModeKey="configs"
      fetchFn={(params, signal) => api.configs(params, signal)}
      keyFn={({ ID }) => ID}
      itemPath={({ ID }) => `/configs/${ID}`}
      columns={(sortKey, sortDir, toggle) => [
        {
          ...sortColumn("Name", "name", sortKey, sortDir, toggle),
          cell: ({ ID, Spec: { Name } }) => <ResourceName name={Name || ID} />,
        },
        {
          ...sortColumn("Created", "created", sortKey, sortDir, toggle),
          cell: ({ CreatedAt }) => (CreatedAt ? <TimeAgo date={CreatedAt} /> : "\u2014"),
        },
        {
          ...sortColumn("Updated", "updated", sortKey, sortDir, toggle),
          cell: ({ UpdatedAt }) => (UpdatedAt ? <TimeAgo date={UpdatedAt} /> : "\u2014"),
        },
      ]}
      renderCard={({ CreatedAt, ID, Spec: { Name }, UpdatedAt }) => (
        <ResourceCard
          title={<ResourceName name={Name || ID} />}
          to={`/configs/${ID}`}
        >
          <div className="space-y-1 text-xs text-muted-foreground">
            <div>Created: {CreatedAt ? <TimeAgo date={CreatedAt} /> : "\u2014"}</div>
            <div>Updated: {UpdatedAt ? <TimeAgo date={UpdatedAt} /> : "\u2014"}</div>
          </div>
        </ResourceCard>
      )}
      emptyMessage={(hasSearch) =>
        hasSearch ? "No configs match your search" : "No configs found"
      }
      actions={(allowedMethods) => (
        <CreateDataResourceForm
          resourceType="Config"
          basePath="/configs"
          canCreate={allowedMethods.has("POST")}
          onCreate={async (name, data) => {
            const response = await api.createConfig(name, data);
            return { id: response.config.ID };
          }}
        />
      )}
    />
  );
}
