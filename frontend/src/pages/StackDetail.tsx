import { useParams, Link } from "react-router-dom";
import { useState, useEffect } from "react";
import { api } from "../api/client";
import type { StackDetail as StackDetailType } from "../api/types";
import PageHeader from "../components/PageHeader";
import { LoadingDetail } from "../components/LoadingSkeleton";

export default function StackDetail() {
  const { name } = useParams<{ name: string }>();
  const [stack, setStack] = useState<StackDetailType | null>(null);

  useEffect(() => {
    if (name) {
      api.stack(name).then(setStack);
    }
  }, [name]);

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
    <div>
      <PageHeader
        title={stack.name}
        subtitle={subtitle}
        breadcrumbs={[{ label: "Stacks", to: "/stacks" }, { label: stack.name }]}
      />
      <div className="space-y-6">
        {stack.services?.length > 0 && (
          <div>
            <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
              Services
            </h2>
            <div className="overflow-x-auto rounded-lg border">
              <table className="w-full">
                <thead className="sticky top-0 z-10 bg-background">
                  <tr className="border-b bg-muted/50">
                    <th className="text-left p-3 text-sm font-medium">Name</th>
                    <th className="text-left p-3 text-sm font-medium">Image</th>
                    <th className="text-left p-3 text-sm font-medium">Mode</th>
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
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {stack.configs?.length > 0 && (
          <div>
            <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
              Configs
            </h2>
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
          </div>
        )}

        {stack.secrets?.length > 0 && (
          <div>
            <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
              Secrets
            </h2>
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
          </div>
        )}

        {stack.networks?.length > 0 && (
          <div>
            <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
              Networks
            </h2>
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
          </div>
        )}

        {stack.volumes?.length > 0 && (
          <div>
            <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
              Volumes
            </h2>
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
          </div>
        )}
      </div>
    </div>
  );
}
