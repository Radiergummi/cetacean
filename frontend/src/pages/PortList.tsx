import { api } from "../api/client";
import type { PublishedPort } from "../api/types";
import ResourceCard from "../components/ResourceCard";
import ResourceListPage from "../components/ResourceListPage";
import ResourceName from "../components/ResourceName";
import { sortColumn } from "../lib/sortColumn";

function boundOn({ publishMode, nodes }: PublishedPort): string {
  if (publishMode !== "host") {
    return "every node";
  }

  const count = nodes?.length ?? 0;

  return count === 1 ? "1 node" : `${count} nodes`;
}

function portLabel({ publishedPort, protocol }: PublishedPort): string {
  return `${publishedPort}/${protocol}`;
}

export default function PortList() {
  return (
    <ResourceListPage<PublishedPort>
      title="Published Ports"
      path="/ports"
      sseType="service,task"
      derived
      defaultSort="published"
      searchPlaceholder="Search by service…"
      viewModeKey="ports"
      fetchFn={(params, signal) => api.ports(params, signal)}
      keyFn={({ serviceId, publishedPort, protocol, publishMode }) =>
        `${serviceId}-${publishedPort}-${protocol}-${publishMode}`
      }
      itemPath={({ serviceId }) => `/services/${serviceId}`}
      columns={(sortKey, sortDir, toggle) => [
        {
          ...sortColumn("Published", "published", sortKey, sortDir, toggle),
          cell: (port) => <span className="font-mono">{portLabel(port)}</span>,
        },
        {
          ...sortColumn("Target", "target", sortKey, sortDir, toggle),
          cell: ({ targetPort }) => <span className="font-mono">{targetPort}</span>,
        },
        {
          ...sortColumn("Mode", "mode", sortKey, sortDir, toggle),
          cell: ({ publishMode }) => publishMode,
        },
        {
          ...sortColumn("Service", "service", sortKey, sortDir, toggle),
          cell: ({ serviceName }) => <ResourceName name={serviceName} />,
        },
        {
          ...sortColumn("Stack", "stack", sortKey, sortDir, toggle),
          cell: ({ stack }) => stack ?? "—",
        },
        {
          header: "Bound on",
          cell: (port) => boundOn(port),
        },
      ]}
      renderCard={(port) => (
        <ResourceCard
          title={<span className="font-mono">{portLabel(port)}</span>}
          to={`/services/${port.serviceId}`}
          meta={[port.serviceName, `→ ${port.targetPort}`, port.publishMode, boundOn(port)]}
        />
      )}
      emptyMessage={(hasSearch) =>
        hasSearch ? "No published ports match your search" : "No service publishes a port"
      }
    />
  );
}
