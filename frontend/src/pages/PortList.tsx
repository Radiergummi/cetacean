import { api } from "../api/client";
import type { PublishedPort, Task } from "../api/types";
import ResourceCard from "../components/ResourceCard";
import ResourceListPage from "../components/ResourceListPage";
import ResourceName from "../components/ResourceName";
import type { SSEEvent } from "../hooks/useResourceStream";
import { sortColumn } from "../lib/sortColumn";

function boundOn({ PublishMode, Nodes }: PublishedPort): string {
  if (PublishMode !== "host") {
    return "every node";
  }

  const count = Nodes?.length ?? 0;

  return count === 1 ? "1 node" : `${count} nodes`;
}

function portLabel({ PublishedPort, Protocol }: PublishedPort): string {
  return `${PublishedPort}/${Protocol}`;
}

/**
 * Only a task holding a host port can move a row; any other task event, which a
 * busy cluster sends constantly, would refetch the list for nothing. A task
 * removed without a payload might have held one.
 */
function changesPorts({ type, resource }: SSEEvent): boolean {
  if (type !== "task" || !resource) {
    return true;
  }

  return ((resource as Task).Status?.PortStatus?.Ports ?? []).some(
    ({ PublishMode }) => PublishMode === "host",
  );
}

export default function PortList() {
  return (
    <ResourceListPage<PublishedPort>
      title="Published Ports"
      path="/ports"
      sseType="service,task"
      derived
      relevantEvent={changesPorts}
      defaultSort="published"
      searchPlaceholder="Search by service…"
      viewModeKey="ports"
      fetchFn={(params, signal) => api.ports(params, signal)}
      keyFn={({ ServiceID, PublishedPort, Protocol, TargetPort }) =>
        `${ServiceID}-${PublishedPort}-${Protocol}-${TargetPort}`
      }
      itemPath={({ ServiceID }) => `/services/${ServiceID}`}
      columns={(sortKey, sortDir, toggle) => [
        {
          ...sortColumn("Published", "published", sortKey, sortDir, toggle),
          cell: (port) => <span className="font-mono">{portLabel(port)}</span>,
        },
        {
          ...sortColumn("Target", "target", sortKey, sortDir, toggle),
          cell: ({ TargetPort }) => <span className="font-mono">{TargetPort}</span>,
        },
        {
          ...sortColumn("Mode", "mode", sortKey, sortDir, toggle),
          cell: ({ PublishMode }) => PublishMode,
        },
        {
          ...sortColumn("Service", "service", sortKey, sortDir, toggle),
          cell: ({ ServiceName }) => <ResourceName name={ServiceName} />,
        },
        {
          ...sortColumn("Stack", "stack", sortKey, sortDir, toggle),
          cell: ({ Stack }) => Stack ?? "—",
        },
        {
          header: "Bound on",
          cell: (port) => boundOn(port),
        },
      ]}
      renderCard={(port) => (
        <ResourceCard
          title={<span className="font-mono">{portLabel(port)}</span>}
          to={`/services/${port.ServiceID}`}
          meta={[port.ServiceName, `→ ${port.TargetPort}`, port.PublishMode, boundOn(port)]}
        />
      )}
      emptyMessage={(hasSearch) =>
        hasSearch ? "No published ports match your search" : "No service publishes a port"
      }
    />
  );
}

export { changesPorts };
