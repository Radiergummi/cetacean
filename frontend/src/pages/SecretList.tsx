import { api } from "../api/client";
import type { SecretListItem } from "../api/types";
import CreateDataResourceForm from "../components/CreateDataResourceForm";
import ResourceCard from "../components/ResourceCard";
import ResourceListPage from "../components/ResourceListPage";
import {
  OlderVersionsToggle,
  UsedBy,
  VersionedName,
  useVersionExpansion,
  type VersionRow,
} from "../components/ResourceVersions";
import TimeAgo from "../components/TimeAgo";
import { sortColumn } from "../lib/sortColumn";
import { useCallback } from "react";

type Row = VersionRow<SecretListItem>;

export default function SecretList() {
  const {
    expanded,
    toggle: toggleVersions,
    arrange,
    follow,
    onEvent,
  } = useVersionExpansion(
    "/secrets",
    useCallback((id: string, signal: AbortSignal) => api.secretVersions(id, signal), []),
  );

  return (
    <ResourceListPage<Row>
      title="Secrets"
      path="/secrets"
      sseType="secret"
      derived
      defaultSort="name"
      searchPlaceholder="Search secrets…"
      viewModeKey="secrets"
      fetchFn={(params, signal) => api.secrets(params, signal)}
      arrange={arrange}
      onItems={follow}
      onEvent={onEvent}
      keyFn={({ ID }) => ID}
      itemPath={({ ID }) => `/secrets/${ID}`}
      columns={(sortKey, sortDir, toggle) => [
        {
          ...sortColumn("Name", "name", sortKey, sortDir, toggle),
          cell: (item) => (
            <span className="flex items-center gap-2">
              <VersionedName item={item} />
              <OlderVersionsToggle
                item={item}
                expanded={expanded}
                onToggle={toggleVersions}
              />
            </span>
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
      renderCard={(item) => {
        const { CreatedAt, ID, UpdatedAt } = item;

        return (
          <div className="space-y-2">
            <ResourceCard
              title={<VersionedName item={item} />}
              to={`/secrets/${ID}`}
            >
              <div className="flex flex-col gap-1 text-xs text-muted-foreground">
                <span>
                  Used by: <UsedBy item={item} />
                </span>
                <span>Created: {CreatedAt ? <TimeAgo date={CreatedAt} /> : "\u2014"}</span>
                <span>Updated: {UpdatedAt ? <TimeAgo date={UpdatedAt} /> : "\u2014"}</span>
              </div>
            </ResourceCard>
            <OlderVersionsToggle
              item={item}
              expanded={expanded}
              onToggle={toggleVersions}
            />
          </div>
        );
      }}
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
