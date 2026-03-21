import { api } from "../api/client";
import type { StackDetail as StackDetailType, Task } from "../api/types";
import CollapsibleSection from "../components/CollapsibleSection";
import FetchError from "../components/FetchError";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import ResourceName from "../components/ResourceName";
import SimpleTable from "../components/SimpleTable";
import { StackActions } from "../components/stack-detail/StackActions";
import { useResourceStream } from "../hooks/useResourceStream";
import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";

export default function StackDetail() {
  const { name } = useParams<{ name: string }>();
  const [stack, setStack] = useState<StackDetailType | null>(null);
  const [error, setError] = useState(false);
  const [taskCounts, setTaskCounts] = useState<
    Record<string, { running: number; desired: number }>
  >({});

  const fetchData = useCallback(() => {
    if (name) {
      api
        .stack(name)
        .then(setStack)
        .catch(() => setError(true));
    }
  }, [name]);

  useEffect(fetchData, [fetchData]);

  useResourceStream(`/stacks/${name}`, fetchData);

  useEffect(() => {
    if (!stack?.services?.length) {
      return;
    }

    const controller = new AbortController();

    Promise.all(
      stack.services.map((service) =>
        api
          .serviceTasks(service.ID, controller.signal)
          .then((tasks: Task[]) => [service, tasks] as const)
          .catch(() => [service, []] as const),
      ),
    ).then((results) => {
      if (controller.signal.aborted) {
        return;
      }

      const counts: Record<string, { running: number; desired: number }> = {};

      for (const [service, tasks] of results) {
        const desired =
          service.Spec.Mode.Replicated?.Replicas ??
          (service.Spec.Mode.Global
            ? tasks.filter(({ Status: { State } }) => State === "running").length
            : 1);

        counts[service.ID] = {
          running: tasks.filter(({ Status: { State } }) => State === "running").length,
          desired,
        };
      }
      setTaskCounts(counts);
    });

    return () => controller.abort();
  }, [stack]);

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
            resourceCounts={{
              services: stack.services.length,
              networks: stack.networks.length,
              configs: stack.configs.length,
              secrets: stack.secrets.length,
            }}
          />
        }
      />

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
                  {TaskTemplate.ContainerSpec.Image.split("@")[0]}
                </td>
                <td className="p-3 text-sm">{Mode.Replicated ? "replicated" : "global"}</td>
                <td className="p-3 text-sm tabular-nums">
                  {taskCounts[ID] ? (
                    <span>
                      <span
                        data-healthy={taskCounts[ID].running >= taskCounts[ID].desired || undefined}
                        className="text-yellow-600 data-healthy:text-green-600"
                      >
                        {taskCounts[ID].running}
                      </span>
                      /{taskCounts[ID].desired}
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
                  to={`/volumes/${Name}`}
                  className="font-medium text-link hover:underline"
                >
                  <ResourceName name={Name} />
                </Link>
              </td>
            )}
          />
        </CollapsibleSection>
      )}
    </div>
  );
}
