import { api } from "../api/client";
import type { ConfigListItem } from "../api/types";
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

type Row = VersionRow<ConfigListItem>;

export default function ConfigList() {
  const {
    expanded,
    toggle: toggleVersions,
    arrange,
  } = useVersionExpansion(
    "/configs",
    useCallback(
      async (id: string, signal: AbortSignal) =>
        (await api.configs({ versionsOf: id }, signal)).data.items,
      [],
    ),
  );

  return (
    <ResourceListPage<Row>
      title="Configs"
      path="/configs"
      sseType="config,service"
      derived
      defaultSort="name"
      searchPlaceholder="Search configs…"
      viewModeKey="configs"
      fetchFn={(params, signal) => api.configs(params, signal)}
      arrange={arrange}
      keyFn={({ ID }) => ID}
      itemPath={({ ID }) => `/configs/${ID}`}
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
              to={`/configs/${ID}`}
            >
              <div className="space-y-1 text-xs text-muted-foreground">
                <div>
                  Used by: <UsedBy item={item} />
                </div>
                <div>Created: {CreatedAt ? <TimeAgo date={CreatedAt} /> : "\u2014"}</div>
                <div>Updated: {UpdatedAt ? <TimeAgo date={UpdatedAt} /> : "\u2014"}</div>
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
