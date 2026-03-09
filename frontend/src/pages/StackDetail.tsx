import type React from "react";
import { useParams, Link } from "react-router-dom";
import { useState, useEffect } from "react";
import { api } from "../api/client";
import type { StackDetail as StackDetailType, Task } from "../api/types";
import PageHeader from "../components/PageHeader";
import { LoadingDetail } from "../components/LoadingSkeleton";
import FetchError from "../components/FetchError";

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
        {title}
      </h2>
      {children}
    </div>
  );
}


export default function StackDetail() {
  const { name } = useParams<{ name: string }>();
  const [stack, setStack] = useState<StackDetailType | null>(null);
  const [error, setError] = useState(false);
  const [taskCounts, setTaskCounts] = useState<Record<string, { running: number; total: number }>>(
    {},
  );

  useEffect(() => {
    if (name) {
      api
        .stack(name)
        .then(setStack)
        .catch(() => setError(true));
    }
  }, [name]);

  useEffect(() => {
    if (!stack?.services?.length) return;
    let cancelled = false;
    Promise.all(
      stack.services.map((svc) =>
        api
          .serviceTasks(svc.ID)
          .then((tasks: Task[]) => [svc.ID, tasks] as const)
          .catch(() => [svc.ID, []] as const),
      ),
    ).then((results) => {
      if (cancelled) return;
      const counts: Record<string, { running: number; total: number }> = {};
      for (const [id, tasks] of results) {
        counts[id] = {
          running: tasks.filter((t) => t.Status.State === "running").length,
          total: tasks.length,
        };
      }
      setTaskCounts(counts);
    });
    return () => {
      cancelled = true;
    };
  }, [stack]);

  if (error) return <FetchError message="Failed to load stack" />;
  if (!stack) return <LoadingDetail />;

  const parts: string[] = [];
  if (stack.services?.length)
    parts.push(`${stack.services.length} service${stack.services.length !== 1 ? "s" : ""}`);
  if (stack.configs?.length)
    parts.push(`${stack.configs.length} config${stack.configs.length !== 1 ? "s" : ""}`);
  if (stack.secrets?.length)
    parts.push(`${stack.secrets.length} secret${stack.secrets.length !== 1 ? "s" : ""}`);
  if (stack.networks?.length)
    parts.push(`${stack.networks.length} network${stack.networks.length !== 1 ? "s" : ""}`);
  if (stack.volumes?.length)
    parts.push(`${stack.volumes.length} volume${stack.volumes.length !== 1 ? "s" : ""}`);
  const subtitle = parts.join(", ");

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={stack.name}
        subtitle={subtitle}
        breadcrumbs={[{ label: "Stacks", to: "/stacks" }, { label: stack.name }]}
      />

      {stack.services?.length > 0 && (
        <Section title="Services">
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full">
              <thead className="sticky top-0 z-10 bg-background">
                <tr className="border-b bg-muted/50">
                  <th className="text-left p-3 text-sm font-medium">Name</th>
                  <th className="text-left p-3 text-sm font-medium">Image</th>
                  <th className="text-left p-3 text-sm font-medium">Mode</th>
                  <th className="text-left p-3 text-sm font-medium">Tasks</th>
                </tr>
              </thead>
              <tbody>
                {stack.services.map((svc) => (
                  <tr key={svc.ID} className="border-b last:border-b-0">
                    <td className="p-3 text-sm">
                      <Link
                        to={`/services/${svc.ID}`}
                        className="text-link hover:underline font-medium"
                      >
                        {svc.Spec.Name || svc.ID}
                      </Link>
                    </td>
                    <td className="p-3 text-sm font-mono text-xs">
                      {svc.Spec.TaskTemplate.ContainerSpec.Image.split("@")[0]}
                    </td>
                    <td className="p-3 text-sm">
                      {svc.Spec.Mode.Replicated ? "replicated" : "global"}
                    </td>
                    <td className="p-3 text-sm tabular-nums">
                      {taskCounts[svc.ID] ? (
                        <span>
                          <span
                            className={
                              taskCounts[svc.ID].running === taskCounts[svc.ID].total
                                ? "text-green-600"
                                : "text-yellow-600"
                            }
                          >
                            {taskCounts[svc.ID].running}
                          </span>
                          /{taskCounts[svc.ID].total}
                        </span>
                      ) : (
                        "—"
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Section>
      )}

      {stack.configs?.length > 0 && (
        <Section title="Configs">
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full">
              <tbody>
                {stack.configs.map((c) => (
                  <tr key={c.ID} className="border-b last:border-b-0">
                    <td className="p-3 text-sm">{c.Spec.Name || c.ID}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Section>
      )}

      {stack.secrets?.length > 0 && (
        <Section title="Secrets">
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full">
              <tbody>
                {stack.secrets.map((s) => (
                  <tr key={s.ID} className="border-b last:border-b-0">
                    <td className="p-3 text-sm">{s.Spec.Name || s.ID}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Section>
      )}

      {stack.networks?.length > 0 && (
        <Section title="Networks">
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full">
              <thead className="sticky top-0 z-10 bg-background">
                <tr className="border-b bg-muted/50">
                  <th className="text-left p-3 text-sm font-medium">Name</th>
                  <th className="text-left p-3 text-sm font-medium">Driver</th>
                </tr>
              </thead>
              <tbody>
                {stack.networks.map((n) => (
                  <tr key={n.Id} className="border-b last:border-b-0">
                    <td className="p-3 text-sm">{n.Name}</td>
                    <td className="p-3 text-sm">{n.Driver}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Section>
      )}

      {stack.volumes?.length > 0 && (
        <Section title="Volumes">
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full">
              <tbody>
                {stack.volumes.map((v) => (
                  <tr key={v.Name} className="border-b last:border-b-0">
                    <td className="p-3 text-sm">{v.Name}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Section>
      )}
    </div>
  );
}
