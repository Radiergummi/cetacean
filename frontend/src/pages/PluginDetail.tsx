import { api } from "../api/client";
import type { Plugin } from "../api/types";
import CollapsibleSection from "../components/CollapsibleSection";
import { ContainerImage, KVTable, MetadataGrid, ResourceId } from "../components/data";
import FetchError from "../components/FetchError";
import InfoCard from "../components/InfoCard";
import InstallPluginDialog from "../components/InstallPluginDialog";
import { PluginEnvEditor } from "../components/PluginEnvEditor";
import { DockerDocsLink } from "../components/service-detail/DockerDocsLink";
import { EditablePanel } from "../components/service-detail/EditablePanel";
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
import { joinCommand, parseCommand } from "../lib/parseCommand";
import { ArrowUpCircle, Power, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

const pluginTypeLabels: Record<string, string> = {
  "docker.volumedriver/1.0": "Volume Driver",
  "docker.networkdriver/1.0": "Network Driver",
  "docker.ipamdriver/1.0": "IPAM Driver",
  "docker.authz/1.0": "Authorization",
  "docker.logdriver/1.0": "Log Driver",
  "docker.metricscollector/1.0": "Metrics Collector",
};

export default function PluginDetail() {
  const { name: rawName } = useParams<{ name: string }>();
  const name = decodeURIComponent(rawName!);
  const navigate = useNavigate();
  const { level } = useOperationsLevel();

  const [plugin, setPlugin] = useState<Plugin | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [upgradeOpen, setUpgradeOpen] = useState(false);
  const [draftArgs, setDraftArgs] = useState("");

  const enableAction = useAsyncAction();
  const removeAction = useAsyncAction();

  const fetchPlugin = useCallback(() => {
    setError(null);
    api
      .plugin(name)
      .then((result) => {
        setPlugin(result);
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
            {level >= opsLevel.configuration && !plugin.Enabled && (
              <Button
                variant="secondary"
                size="sm"
                disabled={enableAction.loading}
                onClick={() =>
                  void enableAction.execute(async () => {
                    await api.enablePlugin(name);
                    fetchPlugin();
                  }, "Failed to enable plugin")
                }
              >
                {enableAction.loading ? (
                  <Spinner className="size-3" />
                ) : (
                  <Power className="size-3.5" />
                )}
                Enable
              </Button>
            )}

            {level >= opsLevel.configuration && plugin.Enabled && (
              <AlertDialog>
                <AlertDialogTrigger
                  render={
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={enableAction.loading}
                    >
                      {enableAction.loading ? (
                        <Spinner className="size-3" />
                      ) : (
                        <Power className="size-3.5" />
                      )}
                      Disable
                    </Button>
                  }
                />
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>Disable plugin?</AlertDialogTitle>
                    <AlertDialogDescription>
                      This will disable the plugin <strong>{name}</strong>. Services using this plugin
                      may be affected.
                    </AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogCancel>Cancel</AlertDialogCancel>
                    <AlertDialogAction
                      onClick={() =>
                        void enableAction.execute(async () => {
                          await api.disablePlugin(name);
                          fetchPlugin();
                        }, "Failed to disable plugin")
                      }
                    >
                      Disable
                    </AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
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
        <ContainerImage
          label="Reference"
          image={plugin.PluginReference}
        />
        <InfoCard
          label="Docker Version"
          value={config.DockerVersion || "—"}
        />
        <InfoCard
          label="Status"
          value={
            <span
              className={
                plugin.Enabled
                  ? "text-green-700 dark:text-green-400"
                  : "text-muted-foreground"
              }
            >
              {plugin.Enabled ? "Enabled" : "Disabled"}
            </span>
          }
        />
        <InfoCard
          label="Type"
          value={
            config.Interface.Types
              ?.map((type) => pluginTypeLabels[type] ?? type)
              .join(", ") || "—"
          }
        />
      </MetadataGrid>

      {/* Settings */}
      <CollapsibleSection
        title="Settings"
        controls={
          <DockerDocsLink href="https://docs.docker.com/reference/cli/docker/plugin/set/" variant="label" />
        }
      >
        <div className="grid gap-4 lg:grid-cols-2">
          <EditablePanel
            title="Args"
            empty={args.length === 0}
            emptyDescription="Click Edit to configure plugin arguments."
            onOpen={() => setDraftArgs(joinCommand(args))}
            onSave={async () => {
              const parsed = parseCommand(draftArgs);
              await api.configurePlugin(name, { args: parsed });
              fetchPlugin();
            }}
            display={
              <code className="block rounded-lg bg-muted/50 p-3 font-mono text-xs">
                {joinCommand(args)}
              </code>
            }
            edit={
              <label className="block space-y-1">
                <span className="text-xs text-muted-foreground">
                  Args (space-separated, use quotes for values with spaces)
                </span>
                <input
                  type="text"
                  value={draftArgs}
                  onChange={(event) => setDraftArgs(event.target.value)}
                  placeholder='--arg1 value1 --arg2 "value with spaces"'
                  className="h-8 w-full rounded-md border bg-transparent px-2 font-mono text-xs outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
                />
              </label>
            }
          />

          <PluginEnvEditor
            pluginName={name}
            declarations={configEnv}
            values={settings.Env ?? []}
            onSaved={fetchPlugin}
          />
        </div>
      </CollapsibleSection>

      {/* Configuration */}
      <CollapsibleSection
        title="Configuration"
        controls={
          <DockerDocsLink href="https://docs.docker.com/engine/extend/config/" variant="label" />
        }
      >
        <div className="space-y-4">
          <KVTable
            rows={[
              ["Entrypoint", entrypoint.join(" ") || "—"],
              ["Working Directory", config.WorkDir || "—"],
              config.User?.UID != null && ["User", `${config.User.UID}:${config.User.GID}`],
              ["Network Type", config.Network.Type || "—"],
              ["Interface Socket", config.Interface.Socket || "—"],
            ]}
          />

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
