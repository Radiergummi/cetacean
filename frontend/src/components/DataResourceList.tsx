import { api, type ListParams } from "../api/client";
import type { ConfigListItem, SecretListItem } from "../api/types";
import { sortColumn } from "../lib/sortColumn";
import CreateDataResourceForm from "./CreateDataResourceForm";
import ResourceCard from "./ResourceCard";
import ResourceListPage from "./ResourceListPage";
import {
  OlderVersionsToggle,
  UsedBy,
  VersionedName,
  useVersionExpansion,
  type VersionRow,
} from "./ResourceVersions";
import TimeAgo from "./TimeAgo";

type Item = ConfigListItem | SecretListItem;
type Row = VersionRow<Item>;

const kinds = {
  config: {
    title: "Configs",
    noun: "configs",
    resourceType: "Config",
    path: "/configs",
    list: (params: ListParams, signal: AbortSignal) => api.configs(params, signal),
    versions: (id: string, signal: AbortSignal): Promise<Item[]> => api.configVersions(id, signal),
    create: async (name: string, data: string) => ({
      id: (await api.createConfig(name, data)).config.ID,
    }),
  },
  secret: {
    title: "Secrets",
    noun: "secrets",
    resourceType: "Secret",
    path: "/secrets",
    list: (params: ListParams, signal: AbortSignal) => api.secrets(params, signal),
    versions: (id: string, signal: AbortSignal): Promise<Item[]> => api.secretVersions(id, signal),
    create: async (name: string, data: string) => ({
      id: (await api.createSecret(name, data)).secret.ID,
    }),
  },
} as const;

/**
 * The list page of a config or secret: the two differ only in where they live.
 */
export default function DataResourceList({ kind }: { kind: keyof typeof kinds }) {
  const { title, noun, resourceType, path, list, versions, create } = kinds[kind];
  const {
    expanded,
    toggle: toggleVersions,
    arrange,
    follow,
    onEvent,
  } = useVersionExpansion(path, versions);

  return (
    <ResourceListPage<Row>
      title={title}
      path={path}
      sseType={kind}
      derived
      defaultSort="name"
      searchPlaceholder={`Search ${noun}…`}
      viewModeKey={noun}
      fetchFn={list}
      arrange={arrange}
      onItems={follow}
      onEvent={onEvent}
      keyFn={({ ID }) => ID}
      itemPath={({ ID }) => `${path}/${ID}`}
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
          cell: ({ CreatedAt }) => (CreatedAt ? <TimeAgo date={CreatedAt} /> : "—"),
        },
        {
          ...sortColumn("Updated", "updated", sortKey, sortDir, toggle),
          cell: ({ UpdatedAt }) => (UpdatedAt ? <TimeAgo date={UpdatedAt} /> : "—"),
        },
      ]}
      renderCard={(item) => {
        const { CreatedAt, ID, UpdatedAt } = item;

        return (
          <div className="space-y-2">
            <ResourceCard
              title={<VersionedName item={item} />}
              to={`${path}/${ID}`}
            >
              <div className="space-y-1 text-xs text-muted-foreground">
                <div>
                  Used by: <UsedBy item={item} />
                </div>
                <div>Created: {CreatedAt ? <TimeAgo date={CreatedAt} /> : "—"}</div>
                <div>Updated: {UpdatedAt ? <TimeAgo date={UpdatedAt} /> : "—"}</div>
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
        hasSearch ? `No ${noun} match your search` : `No ${noun} found`
      }
      actions={(allowedMethods) => (
        <CreateDataResourceForm
          resourceType={resourceType}
          basePath={path}
          canCreate={allowedMethods.has("POST")}
          onCreate={create}
        />
      )}
    />
  );
}
