import { api, emptyMethods } from "../api/client";
import FetchError from "../components/FetchError";
import InstallPluginDialog from "../components/InstallPluginDialog";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import PluginTable from "../components/PluginTable";
import { Button } from "../components/ui/button";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useState } from "react";

export default function PluginList() {
  const queryClientInstance = useQueryClient();
  const [installOpen, setInstallOpen] = useState(false);

  const {
    data: pluginResult,
    error: queryError,
    isLoading,
    refetch,
  } = useQuery({
    queryKey: ["plugins"],
    queryFn: () => api.plugins(),
    retry: false,
  });

  const plugins = pluginResult?.data ?? null;
  const allowedMethods = pluginResult?.allowedMethods ?? emptyMethods;
  const error = queryError
    ? queryError instanceof Error
      ? queryError.message
      : "Failed to load plugins"
    : null;

  if (error) {
    return (
      <FetchError
        message={error}
        onRetry={() => refetch()}
      />
    );
  }

  if (isLoading || !plugins) {
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
        onInstalled={() => void queryClientInstance.invalidateQueries({ queryKey: ["plugins"] })}
      />
    </div>
  );
}
