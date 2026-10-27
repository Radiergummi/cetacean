import { api } from "../api/client";
import type { SecretListItem } from "../api/types";
import CreateDataResourceForm from "../components/CreateDataResourceForm";
import ResourceCard from "../components/ResourceCard";
import ResourceListPage from "../components/ResourceListPage";
import { UsedBy, VersionedName, useExpandedVersions } from "../components/ResourceVersions";
import TimeAgo from "../components/TimeAgo";
import { sortColumn } from "../lib/sortColumn";
import { arrangeVersions, type VersionPlacement } from "../lib/versionGroups";

type Row = SecretListItem & VersionPlacement;

export default function SecretList() {
  const { expanded, toggle: toggleVersions } = useExpandedVersions();

  return (
    <ResourceListPage<Row>
      title="Secrets"
      path="/secrets"
      sseType="secret"
      defaultSort="name"
      searchPlaceholder="Search secrets…"
      viewModeKey="secrets"
      fetchFn={(params, signal) => api.secrets(params, signal)}
      arrange={(items) => arrangeVersions(items, expanded)}
      keyFn={({ ID }) => ID}
      itemPath={({ ID }) => `/secrets/${ID}`}
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
          to={`/secrets/${item.ID}`}
        >
          <div className="flex flex-col gap-1 text-xs text-muted-foreground">
            <span>
              Used by: <UsedBy item={item} />
            </span>
            <span>Created: {item.CreatedAt ? <TimeAgo date={item.CreatedAt} /> : "\u2014"}</span>
            <span>Updated: {item.UpdatedAt ? <TimeAgo date={item.UpdatedAt} /> : "\u2014"}</span>
          </div>
        </ResourceCard>
      )}
      emptyMessage={(hasSearch) =>
        hasSearch ? "No secrets match your search" : "No secrets found"
      }
      actions={(allowedMethods) => (
        <CreateDataResourceForm
          resourceType="Secret"
          basePath="/secrets"
          canCreate={allowedMethods.has("POST")}
          onCreate={async (name, data) => {
            const response = await api.createSecret(name, data);
            return { id: response.secret.ID };
          }}
        />
      )}
    />
  );
}
