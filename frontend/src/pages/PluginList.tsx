import { api, emptyMethods } from "../api/client";
import type { Plugin } from "../api/types";
import FetchError from "../components/FetchError";
import InstallPluginDialog from "../components/InstallPluginDialog";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import PluginTable from "../components/PluginTable";
import { Button } from "../components/ui/button";
import { Plus } from "lucide-react";
import { useCallback, useEffect, useState } from "react";

export default function PluginList() {
  const [plugins, setPlugins] = useState<Plugin[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [installOpen, setInstallOpen] = useState(false);
  const [allowedMethods, setAllowedMethods] = useState(emptyMethods);

  const fetchPlugins = useCallback(() => {
    setError(null);
    api
      .plugins()
      .then(({ data: pluginsData, allowedMethods: methods }) => {
        setPlugins(pluginsData);
        setAllowedMethods(methods);
      })
      .catch((thrown) => {
        setError(thrown instanceof Error ? thrown.message : "Failed to load plugins");
      });
  }, []);

  useEffect(() => {
    fetchPlugins();
  }, [fetchPlugins]);

  if (error) {
    return (
      <FetchError
        message={error}
        onRetry={fetchPlugins}
      />
    );
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
          allowedMethods.has("POST") ? (
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
        <PluginTable plugins={plugins} />
      )}

      <InstallPluginDialog
        open={installOpen}
        onOpenChange={setInstallOpen}
        onInstalled={fetchPlugins}
      />
    </div>
  );
}
