import { api } from "../api/client";
import type { StackResponse } from "../api/types";
import CollapsibleSection from "../components/CollapsibleSection";
import ComposeSection, { composeQueryKey } from "../components/ComposeSection";
import FetchError from "../components/FetchError";
import { GraphFrame } from "../components/graph/GraphFrame";
import { TaskHealth } from "../components/HealthIndicator";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import ResourceName from "../components/ResourceName";
import ServiceLinks from "../components/ServiceLinks";
import SimpleTable from "../components/SimpleTable";
import { StackActions } from "../components/stack-detail/StackActions";
import { useDetailResource } from "../hooks/useDetailResource";
import { serviceModeLabel } from "../lib/deriveServiceState";
import { desiredTasks } from "../lib/serviceHealth";
import type { TaskCount } from "../lib/stackGraph";
import { useQuery } from "@tanstack/react-query";
import { lazy, useMemo } from "react";
import { Link, useParams } from "react-router-dom";

const StackGraph = lazy(() => import("../components/stack-detail/stack-graph/StackGraph"));

export default function StackDetail() {
  const { name } = useParams<{ name: string }>();

  // Any of the stack's resources changing changes the document it projects,
  // and the running counts its services report.
  const servicesKey = useMemo(() => ["stack-services", name] as const, [name]);
  const extraQueryKeys = useMemo(
    () => [composeQueryKey(`stack:${name}`), servicesKey] as const,
    [name, servicesKey],
  );

  const {
    data: response,
    error,
    allowedMethods,
  } = useDetailResource<StackResponse>(name, api.stack, "/stacks", {
    history: false,
    extraQueryKeys,
  });
  const stack = response?.stack;
  const links = response?.links;

  const { data: members } = useQuery({
    queryKey: servicesKey,
    queryFn: ({ signal }) => api.services({ filter: `stack == ${JSON.stringify(name)}` }, signal),
    enabled: !!stack?.services?.length,
  });

  const taskCounts = useMemo(() => {
    const counts: Record<string, TaskCount> = {};

    for (const { ID, RunningTasks, Spec } of members?.data.items ?? []) {
      counts[ID] = { running: RunningTasks, desired: desiredTasks(Spec.Mode) };
    }

    return counts;
  }, [members]);

  if (error) {
    return <FetchError message="Failed to load stack" />;
  }
  if (!stack) {
    return <LoadingDetail />;
  }

  const parts: string[] = [];
  if (stack.services?.length) {
    parts.push(`${stack.services.length} service${stack.services.length !== 1 ? "s" : ""}`);
  }
  if (stack.configs?.length) {
    parts.push(`${stack.configs.length} config${stack.configs.length !== 1 ? "s" : ""}`);
  }
  if (stack.secrets?.length) {
    parts.push(`${stack.secrets.length} secret${stack.secrets.length !== 1 ? "s" : ""}`);
  }
  if (stack.networks?.length) {
    parts.push(`${stack.networks.length} network${stack.networks.length !== 1 ? "s" : ""}`);
  }
  if (stack.volumes?.length) {
    parts.push(`${stack.volumes.length} volume${stack.volumes.length !== 1 ? "s" : ""}`);
  }
  const subtitle = parts.join(", ");

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={stack.name}
        subtitle={subtitle}
        breadcrumbs={[{ label: "Stacks", to: "/stacks" }, { label: stack.name }]}
        actions={
          <StackActions
            stackName={stack.name}
            allowedMethods={allowedMethods}
            resourceCounts={{
              services: stack.services?.length ?? 0,
              networks: stack.networks?.length ?? 0,
              configs: stack.configs?.length ?? 0,
              secrets: stack.secrets?.length ?? 0,
            }}
          />
        }
        extra={links?.length ? <ServiceLinks links={links} /> : undefined}
      />

      {stack.services?.length > 0 && (
        <CollapsibleSection
          title="Topology"
          defaultOpen
        >
          <GraphFrame>
            <StackGraph
              stack={stack}
              taskCounts={taskCounts}
            />
          </GraphFrame>
        </CollapsibleSection>
      )}

      {stack.services?.length > 0 && (
        <CollapsibleSection title="Services">
          <SimpleTable
            columns={["Name", "Image", "Mode", "Tasks"]}
            items={stack.services}
            keyFn={({ ID }) => ID}
            renderRow={({ ID, Spec: { Mode, Name, TaskTemplate } }) => (
              <>
                <td className="p-3 text-sm">
                  <Link
                    to={`/services/${ID}`}
                    className="font-medium text-link hover:underline"
                  >
                    <ResourceName name={Name || ID} />
                  </Link>
                </td>
                <td className="p-3 font-mono text-xs">
                  {TaskTemplate?.ContainerSpec?.Image?.split("@")[0]}
                </td>
                <td className="p-3 text-sm">{serviceModeLabel(Mode)}</td>
                <td className="p-3 text-sm tabular-nums">
                  {taskCounts[ID] ? (
                    <span>
                      <TaskHealth {...taskCounts[ID]} />
                      {taskCounts[ID].desired !== undefined && `/${taskCounts[ID].desired}`}
                    </span>
                  ) : (
                    "—"
                  )}
                </td>
              </>
            )}
          />
        </CollapsibleSection>
      )}

      {stack.configs?.length > 0 && (
        <CollapsibleSection title="Configs">
          <SimpleTable
            maxHeight
            items={stack.configs}
            keyFn={({ ID }) => ID}
            renderRow={({ ID, Spec: { Name } }) => (
              <td className="p-3 text-sm">
                <Link
                  to={`/configs/${ID}`}
                  className="font-medium text-link hover:underline"
                >
                  <ResourceName name={Name || ID} />
                </Link>
              </td>
            )}
          />
        </CollapsibleSection>
      )}

      {stack.secrets?.length > 0 && (
        <CollapsibleSection title="Secrets">
          <SimpleTable
            maxHeight
            items={stack.secrets}
            keyFn={({ ID }) => ID}
            renderRow={({ ID, Spec: { Name } }) => (
              <td className="p-3 text-sm">
                <Link
                  to={`/secrets/${ID}`}
                  className="font-medium text-link hover:underline"
                >
                  <ResourceName name={Name || ID} />
                </Link>
              </td>
            )}
          />
        </CollapsibleSection>
      )}

      {stack.networks?.length > 0 && (
        <CollapsibleSection title="Networks">
          <SimpleTable
            maxHeight
            columns={["Name", "Driver"]}
            items={stack.networks}
            keyFn={({ Id }) => Id}
            renderRow={({ Driver, Id, Name }) => (
              <>
                <td className="p-3 text-sm">
                  <Link
                    to={`/networks/${Id}`}
                    className="font-medium text-link hover:underline"
                  >
                    <ResourceName name={Name} />
                  </Link>
                </td>
                <td className="p-3 text-sm">{Driver}</td>
              </>
            )}
          />
        </CollapsibleSection>
      )}

      {stack.volumes?.length > 0 && (
        <CollapsibleSection title="Volumes">
          <SimpleTable
            maxHeight
            items={stack.volumes}
            keyFn={({ Name }) => Name}
            renderRow={({ Name }) => (
              <td className="p-3 text-sm">
                <Link
                  to={`/volumes/${encodeURIComponent(Name)}`}
                  className="font-medium text-link hover:underline"
                >
                  <ResourceName name={Name} />
                </Link>
              </td>
            )}
          />
        </CollapsibleSection>
      )}

      <ComposeSection
        name={stack.name}
        queryKey={`stack:${stack.name}`}
        fetcher={(signal) => api.stackCompose(stack.name, signal)}
      />
    </div>
  );
}
