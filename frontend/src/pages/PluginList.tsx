import { api } from "../api/client";
import type { Plugin } from "../api/types";
import FetchError from "../components/FetchError";
import InstallPluginDialog from "../components/InstallPluginDialog";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import { Button } from "../components/ui/button";
import { opsLevel, useOperationsLevel } from "../hooks/useOperationsLevel";
import { Plus } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";

export default function PluginList() {
  const [plugins, setPlugins] = useState<Plugin[] | null>(null);
  const [error, setError] = useState(false);
  const [installOpen, setInstallOpen] = useState(false);
  const { level } = useOperationsLevel();

  const fetchPlugins = useCallback(() => {
    api
      .plugins()
      .then(setPlugins)
      .catch(() => setError(true));
  }, []);

  useEffect(() => {
    fetchPlugins();
  }, [fetchPlugins]);

  if (error) {
    return <FetchError message="Failed to load plugins" />;
  }

  if (!plugins) {
    return <LoadingDetail />;
  }

  return (
    <div>
      <PageHeader
        title="Plugins"
        breadcrumbs={[{ label: "Swarm", to: "/swarm" }, { label: "Plugins" }]}
        actions={
          level >= opsLevel.impactful ? (
            <Button onClick={() => setInstallOpen(true)}>
              <Plus data-icon="inline-start" />
              Install Plugin
            </Button>
          ) : undefined
        }
      />

      {plugins.length === 0 ? (
        <p className="py-12 text-center text-sm text-muted-foreground">No plugins installed</p>
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <table className="w-full">
            <thead>
              <tr className="border-b text-left text-xs font-medium tracking-wider text-muted-foreground uppercase">
                <th className="p-3">Name</th>
                <th className="p-3">Type</th>
                <th className="p-3">Status</th>
              </tr>
            </thead>
            <tbody>
              {plugins.map(({ Config: { Interface }, Enabled, Id, Name }) => (
                <tr
                  key={Id ?? Name}
                  className="border-b last:border-b-0"
                >
                  <td className="p-3 font-mono text-xs">
                    <Link
                      to={`/plugins/${encodeURIComponent(Name)}`}
                      className="text-link hover:underline"
                    >
                      {Name}
                    </Link>
                  </td>
                  <td className="p-3 text-sm text-muted-foreground">
                    {Interface.Types.map(({ Capability }) => Capability).join(", ") || "—"}
                  </td>
                  <td className="p-3">
                    <span
                      data-enabled={Enabled || undefined}
                      className="inline-flex items-center rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground data-enabled:bg-green-500/10 data-enabled:text-green-500"
                    >
                      {Enabled ? "Enabled" : "Disabled"}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <InstallPluginDialog
        open={installOpen}
        onOpenChange={setInstallOpen}
        onInstalled={fetchPlugins}
      />
    </div>
  );
}
