import { api } from "../api/client";
import type { Secret } from "../api/types";
import CreateDataResourceForm from "../components/CreateDataResourceForm";
import ResourceCard from "../components/ResourceCard";
import ResourceListPage from "../components/ResourceListPage";
import ResourceName from "../components/ResourceName";
import TimeAgo from "../components/TimeAgo";
import { sortColumn } from "../lib/sortColumn";

export default function SecretList() {
  return (
    <ResourceListPage<Secret>
      title="Secrets"
      path="/secrets"
      sseType="secret"
      defaultSort="name"
      searchPlaceholder="Search secrets…"
      viewModeKey="secrets"
      fetchFn={(params, signal) => api.secrets(params, signal)}
      keyFn={({ ID }) => ID}
      itemPath={({ ID }) => `/secrets/${ID}`}
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
          to={`/secrets/${ID}`}
        >
          <div className="flex flex-col gap-1 text-xs text-muted-foreground">
            <span>Created: {CreatedAt ? <TimeAgo date={CreatedAt} /> : "\u2014"}</span>
            <span>Updated: {UpdatedAt ? <TimeAgo date={UpdatedAt} /> : "\u2014"}</span>
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
