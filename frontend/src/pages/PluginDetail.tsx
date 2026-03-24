import { api } from "../api/client";
import type { Plugin } from "../api/types";
import CollapsibleSection from "../components/CollapsibleSection";
import { KVTable, MetadataGrid, ResourceId } from "../components/data";
import FetchError from "../components/FetchError";
import InfoCard from "../components/InfoCard";
import InstallPluginDialog from "../components/InstallPluginDialog";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import { Spinner } from "../components/Spinner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "../components/ui/alert-dialog";
import { Button } from "../components/ui/button";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { opsLevel, useOperationsLevel } from "../hooks/useOperationsLevel";
import { ArrowUpCircle, Power, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

export default function PluginDetail() {
  const { name: rawName } = useParams<{ name: string }>();
  const name = decodeURIComponent(rawName!);
  const navigate = useNavigate();
  const { level } = useOperationsLevel();

  const [plugin, setPlugin] = useState<Plugin | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [upgradeOpen, setUpgradeOpen] = useState(false);
  const [draftArgs, setDraftArgs] = useState("");
  const [argsEditing, setArgsEditing] = useState(false);

  const enableAction = useAsyncAction();
  const removeAction = useAsyncAction();
  const configureAction = useAsyncAction();

  const fetchPlugin = useCallback(() => {
    setError(null);
    api
      .plugin(name)
      .then((result) => {
        setPlugin(result);
        setDraftArgs((result.Settings.Args ?? []).join("\n"));
      })
      .catch((thrown) => {
        setError(thrown instanceof Error ? thrown.message : "Failed to load plugin");
      });
  }, [name]);

  useEffect(() => {
    fetchPlugin();
  }, [fetchPlugin]);

  if (error) {
    return (
      <FetchError
        message={error}
        onRetry={fetchPlugin}
      />
    );
  }

  if (!plugin) {
    return <LoadingDetail />;
  }

  const { Config: config, Settings: settings } = plugin;
  const args = settings.Args ?? [];
  const entrypoint = config.Entrypoint ?? [];
  const configEnv = config.Env ?? [];
  const configMounts = config.Mounts ?? [];
  const capabilities = config.Linux?.Capabilities ?? [];
  const linuxDevices = config.Linux?.Devices ?? [];

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={name}
        breadcrumbs={[
          { label: "Swarm", to: "/swarm" },
          { label: "Plugins", to: "/plugins" },
          { label: name },
        ]}
        actions={
          <>
            {level >= opsLevel.configuration && (
              <Button
                variant="secondary"
                size="sm"
                disabled={enableAction.loading}
                onClick={() =>
                  void enableAction.execute(
                    async () => {
                      if (plugin.Enabled) {
                        await api.disablePlugin(name);
                      } else {
                        await api.enablePlugin(name);
                      }

                      fetchPlugin();
                    },
                    `Failed to ${plugin.Enabled ? "disable" : "enable"} plugin`,
                  )
                }
              >
                {enableAction.loading ? (
                  <Spinner className="size-3" />
                ) : (
                  <Power className="size-3.5" />
                )}
                {plugin.Enabled ? "Disable" : "Enable"}
              </Button>
            )}

            {level >= opsLevel.impactful && (
              <Button
                variant="secondary"
                size="sm"
                onClick={() => setUpgradeOpen(true)}
              >
                <ArrowUpCircle className="size-3.5" />
                Upgrade
              </Button>
            )}

            {level >= opsLevel.impactful && (
              <AlertDialog>
                <AlertDialogTrigger
                  render={
                    <Button
                      variant="destructive"
                      size="sm"
                      disabled={removeAction.loading}
                    >
                      {removeAction.loading ? (
                        <Spinner className="size-3" />
                      ) : (
                        <Trash2 className="size-3.5" />
                      )}
                      Remove
                    </Button>
                  }
                />
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>Remove plugin?</AlertDialogTitle>
                    <AlertDialogDescription>
                      This will permanently remove the plugin <strong>{name}</strong>. This action
                      cannot be undone.
                    </AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogCancel>Cancel</AlertDialogCancel>
                    <AlertDialogAction
                      variant="destructive"
                      onClick={() =>
                        void removeAction.execute(async () => {
                          await api.removePlugin(name, true);
                          navigate("/plugins");
                        }, "Failed to remove plugin")
                      }
                    >
                      Remove
                    </AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
            )}
          </>
        }
      />

      {enableAction.error && (
        <p className="text-xs text-red-600 dark:text-red-400">{enableAction.error}</p>
      )}

      {removeAction.error && (
        <p className="text-xs text-red-600 dark:text-red-400">{removeAction.error}</p>
      )}

      {/* Overview */}
      <MetadataGrid>
        {plugin.Id && (
          <ResourceId
            label="ID"
            id={plugin.Id}
          />
        )}
        <InfoCard
          label="Description"
          value={config.Description || "—"}
        />
        <InfoCard
          label="Reference"
          value={plugin.PluginReference || "—"}
        />
        <InfoCard
          label="Docker Version"
          value={config.DockerVersion || "—"}
        />
        <InfoCard
          label="Status"
          value={plugin.Enabled ? "Enabled" : "Disabled"}
        />
        <InfoCard
          label="Type"
          value={config.Interface.Types?.join(", ") || "—"}
        />
      </MetadataGrid>

      {/* Settings: Args */}
      <CollapsibleSection title="Settings">
        <div className="space-y-3">
          {args.length > 0 && !argsEditing && (
            <div className="space-y-1">
              <span className="text-xs font-medium text-muted-foreground">Current Args</span>
              <pre className="rounded-lg bg-muted/50 p-3 font-mono text-xs whitespace-pre-wrap">
                {args.join("\n")}
              </pre>
            </div>
          )}

          {args.length === 0 && !argsEditing && (
            <p className="text-sm text-muted-foreground">No args configured.</p>
          )}

          {argsEditing && (
            <div className="space-y-2">
              <label className="block space-y-1">
                <span className="text-xs text-muted-foreground">Args (one per line)</span>
                <textarea
                  value={draftArgs}
                  onChange={(event) => setDraftArgs(event.target.value)}
                  rows={4}
                  className="w-full rounded-md border bg-transparent px-3 py-2 font-mono text-xs transition outline-none focus:ring-2 focus:ring-ring"
                  disabled={configureAction.loading}
                />
              </label>

              <div className="flex gap-2">
                <Button
                  size="sm"
                  disabled={configureAction.loading}
                  onClick={() =>
                    void configureAction.execute(async () => {
                      const args = draftArgs
                        .split("\n")
                        .map((line) => line.trim())
                        .filter(Boolean);
                      await api.configurePlugin(name, args);
                      setArgsEditing(false);
                      fetchPlugin();
                    }, "Failed to configure plugin")
                  }
                >
                  {configureAction.loading && <Spinner className="size-3" />}
                  Save
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setDraftArgs(args.join("\n"));
                    setArgsEditing(false);
                  }}
                >
                  Cancel
                </Button>
              </div>

              {configureAction.error && (
                <p className="text-xs text-red-600 dark:text-red-400">{configureAction.error}</p>
              )}
            </div>
          )}

          {!argsEditing && level >= opsLevel.configuration && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => setArgsEditing(true)}
            >
              Edit Args
            </Button>
          )}
        </div>
      </CollapsibleSection>

      {/* Configuration */}
      <CollapsibleSection title="Configuration">
        <div className="space-y-4">
          <KVTable
            rows={[
              ["Entrypoint", entrypoint.join(" ") || "—"],
              ["WorkDir", config.WorkDir || "—"],
              config.User && ["User", `${config.User.UID}:${config.User.GID}`],
              ["Network Type", config.Network.Type || "—"],
              ["Interface Socket", config.Interface.Socket || "—"],
            ]}
          />

          {configEnv.length > 0 && (
            <div className="space-y-1">
              <span className="text-xs font-medium text-muted-foreground">
                Environment Variables
              </span>
              <KVTable
                rows={configEnv.map(({ Name: envName, Value, Description }) => [
                  envName,
                  `${Value ?? ""}${Description ? ` — ${Description}` : ""}`,
                ])}
              />
            </div>
          )}

          {configMounts.length > 0 && (
            <div className="space-y-1">
              <span className="text-xs font-medium text-muted-foreground">Mounts</span>
              <KVTable
                rows={configMounts.map(({ Name: mountName, Source, Destination, Type }) => [
                  mountName || Source || "—",
                  `${Source ?? "—"} → ${Destination} (${Type})`,
                ])}
              />
            </div>
          )}

          {capabilities.length > 0 && (
            <div className="space-y-1">
              <span className="text-xs font-medium text-muted-foreground">Linux Capabilities</span>
              <pre className="rounded-lg bg-muted/50 p-3 font-mono text-xs whitespace-pre-wrap">
                {capabilities.join(", ")}
              </pre>
            </div>
          )}

          {linuxDevices.length > 0 && (
            <div className="space-y-1">
              <span className="text-xs font-medium text-muted-foreground">Devices</span>
              <KVTable
                rows={linuxDevices.map(({ Name: deviceName, Path }) => [deviceName, Path ?? "—"])}
              />
            </div>
          )}
        </div>
      </CollapsibleSection>

      <InstallPluginDialog
        open={upgradeOpen}
        onOpenChange={setUpgradeOpen}
        onInstalled={fetchPlugin}
        mode="upgrade"
        pluginName={name}
        currentReference={plugin.PluginReference}
      />
    </div>
  );
}
