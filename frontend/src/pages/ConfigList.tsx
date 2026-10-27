import { api } from "../api/client";
import type { ConfigListItem } from "../api/types";
import CreateDataResourceForm from "../components/CreateDataResourceForm";
import ResourceCard from "../components/ResourceCard";
import ResourceListPage from "../components/ResourceListPage";
import { UsedBy, VersionedName, useExpandedVersions } from "../components/ResourceVersions";
import TimeAgo from "../components/TimeAgo";
import { sortColumn } from "../lib/sortColumn";
import { arrangeVersions, type VersionPlacement } from "../lib/versionGroups";

type Row = ConfigListItem & VersionPlacement;

export default function ConfigList() {
  const { expanded, toggle: toggleVersions } = useExpandedVersions();

  return (
    <ResourceListPage<Row>
      title="Configs"
      path="/configs"
      sseType="config"
      defaultSort="name"
      searchPlaceholder="Search configs…"
      viewModeKey="configs"
      fetchFn={(params, signal) => api.configs(params, signal)}
      arrange={(items) => arrangeVersions(items, expanded)}
      keyFn={({ ID }) => ID}
      itemPath={({ ID }) => `/configs/${ID}`}
      columns={(sortKey, sortDir, toggle) => [
        {
          ...sortColumn("Name", "name", sortKey, sortDir, toggle),
          cell: (item) => (
            <VersionedName
              item={item}
              expanded={expanded}
              onToggle={toggleVersions}
            />
          ),
        },
        {
          header: "Used by",
          cell: (item) => <UsedBy item={item} />,
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
      renderCard={(item) => (
        <ResourceCard
          title={
            <VersionedName
              item={item}
              expanded={expanded}
              onToggle={toggleVersions}
            />
          }
          to={`/configs/${item.ID}`}
        >
          <div className="space-y-1 text-xs text-muted-foreground">
            <div>
              Used by: <UsedBy item={item} />
            </div>
            <div>Created: {item.CreatedAt ? <TimeAgo date={item.CreatedAt} /> : "\u2014"}</div>
            <div>Updated: {item.UpdatedAt ? <TimeAgo date={item.UpdatedAt} /> : "\u2014"}</div>
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
