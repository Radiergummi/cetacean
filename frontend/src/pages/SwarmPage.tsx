import { api } from "../api/client";
import type { Plugin, SwarmInfo } from "../api/types";
import CollapsibleSection from "../components/CollapsibleSection";
import { KVTable, MetadataGrid, ResourceId, Timestamp } from "../components/data";
import FetchError from "../components/FetchError";
import InfoCard from "../components/InfoCard";
import InstallPluginDialog from "../components/InstallPluginDialog";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PluginTable from "../components/PluginTable";
import PageHeader from "../components/PageHeader";
import { Spinner } from "../components/Spinner";
import { SwarmActions } from "../components/swarm-detail/SwarmActions";
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "../components/ui/dialog";
import { DurationInput } from "../components/ui/duration-input";
import { NumberField } from "../components/ui/number-field";
import { Input } from "../components/ui/input";
import { Switch } from "../components/ui/switch";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { opsLevel, useOperationsLevel } from "../hooks/useOperationsLevel";
import { formatDuration } from "../lib/format";
import { EditablePanel } from "@/components/service-detail/EditablePanel";
import { Check, Copy, KeyRound, LockOpen, Plus, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useState } from "react";


function JoinTokenDialog({
  label,
  token,
  managerAddr,
  variant,
}: {
  label: string;
  token: string;
  managerAddr: string;
  variant: "default" | "secondary";
}) {
  const [copied, setCopied] = useState(false);
  const joinCmd = managerAddr ? `docker swarm join --token ${token} ${managerAddr}` : token;

  function copyToClipboard() {
    navigator.clipboard.writeText(joinCmd).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }

  return (
    <Dialog>
      <DialogTrigger
        render={
          <Button
            variant={variant}
            size="sm"
          />
        }
      >
        Join {label}
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Join as {label}</DialogTitle>
          <DialogDescription>
            Run this command on the node you want to join to the swarm.
          </DialogDescription>
        </DialogHeader>
        <pre className="max-w-full rounded-lg bg-muted/50 p-3 font-mono text-xs leading-normal wrap-anywhere break-all whitespace-normal select-all">
          {joinCmd}
        </pre>
        <DialogFooter>
          <Button
            variant="ghost"
            onClick={copyToClipboard}
          >
            {copied ? (
              <>
                <Check data-icon="inline-start" />
                Copied
              </>
            ) : (
              <>
                <Copy data-icon="inline-start" />
                Copy
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default function SwarmPage() {
  const [data, setData] = useState<SwarmInfo | null>(null);
  const [plugins, setPlugins] = useState<Plugin[]>([]);
  const [error, setError] = useState(false);
  const [installOpen, setInstallOpen] = useState(false);
  const { level } = useOperationsLevel();

  const fetchSwarmInfo = useCallback(() => {
    api
      .swarm()
      .then(setData)
      .catch(() => setError(true));
  }, []);

  useEffect(() => {
    fetchSwarmInfo();
    api
      .plugins()
      .then(setPlugins)
      .catch(() => {});
  }, [fetchSwarmInfo]);

  // Orchestration draft
  const [draftTaskHistoryLimit, setDraftTaskHistoryLimit] = useState(0);

  // Raft draft
  const [draftSnapshotInterval, setDraftSnapshotInterval] = useState(0);
  const [draftLogEntries, setDraftLogEntries] = useState(0);
  const [draftKeepOldSnapshots, setDraftKeepOldSnapshots] = useState(0);

  // Dispatcher draft
  const [draftHeartbeatPeriod, setDraftHeartbeatPeriod] = useState(0);

  // CA draft
  const [draftCertExpiry, setDraftCertExpiry] = useState(0);

  // Encryption draft
  const [draftAutoLock, setDraftAutoLock] = useState(false);

  // CA force rotate
  const forceRotateCA = useAsyncAction();

  // Unlock key
  const [unlockKeyValue, setUnlockKeyValue] = useState<string | null>(null);
  const [showUnlockKey, setShowUnlockKey] = useState(false);
  const [unlockKeyCopied, setUnlockKeyCopied] = useState(false);
  const fetchUnlockKey = useAsyncAction();
  const rotateUnlockKey = useAsyncAction();

  // Unlock swarm
  const [unlockInput, setUnlockInput] = useState("");
  const [unlockOpen, setUnlockOpen] = useState(false);
  const unlockSwarm = useAsyncAction();

  if (error) {
    return <FetchError message="Failed to load swarm info" />;
  }
  if (!data) {
    return <LoadingDetail />;
  }

  const { swarm, managerAddr } = data;
  const spec = swarm.Spec;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Swarm"
        actions={
          <>
            <SwarmActions onRotated={fetchSwarmInfo} />
            <JoinTokenDialog
              label="Manager"
              token={swarm.JoinTokens.Manager}
              managerAddr={managerAddr}
              variant="secondary"
            />
            <JoinTokenDialog
              label="Worker"
              token={swarm.JoinTokens.Worker}
              managerAddr={managerAddr}
              variant="default"
            />
          </>
        }
      />

      {/* Overview */}
      <MetadataGrid>
        <ResourceId
          label="Cluster ID"
          id={swarm.ID}
        />
        <Timestamp
          label="Created"
          date={swarm.CreatedAt}
        />
        <Timestamp
          label="Updated"
          date={swarm.UpdatedAt}
        />
        <InfoCard
          label="Default Address Pool"
          value={swarm.DefaultAddrPool?.join(", ") || "—"}
        />
        <InfoCard
          label="Subnet Size"
          value={swarm.SubnetSize ? `/${swarm.SubnetSize}` : "—"}
        />
        <InfoCard
          label="Data Path Port"
          value={swarm.DataPathPort ? String(swarm.DataPathPort) : "—"}
        />
      </MetadataGrid>

      <div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
        {/* Raft */}
        <EditablePanel
          title="Raft"
          requiredLevel={opsLevel.configuration}
          display={
            <KVTable
              rows={[
                ["Snapshot Interval", String(spec.Raft.SnapshotInterval)],
                spec.Raft.KeepOldSnapshots != null && [
                  "Keep Old Snapshots",
                  String(spec.Raft.KeepOldSnapshots),
                ],
                ["Log Entries for Slow Followers", String(spec.Raft.LogEntriesForSlowFollowers)],
                ["Election Tick", `${spec.Raft.ElectionTick} ticks`],
                ["Heartbeat Tick", `${spec.Raft.HeartbeatTick} ticks`],
              ]}
            />
          }
          edit={
            <div className="space-y-3">
              <NumberField
                value={draftSnapshotInterval || undefined}
                onChange={(next) => setDraftSnapshotInterval(next ?? 0)}
                min={0}
                step={1000}
                label="Snapshot Interval"
              />

              <NumberField
                value={draftKeepOldSnapshots || undefined}
                onChange={(next) => setDraftKeepOldSnapshots(next ?? 0)}
                min={0}
                step={1}
                label="Keep Old Snapshots"
              />

              <NumberField
                value={draftLogEntries || undefined}
                onChange={(next) => setDraftLogEntries(next ?? 0)}
                min={0}
                step={100}
                label="Log Entries for Slow Followers"
              />

              <KVTable
                rows={[
                  ["Election Tick", `${spec.Raft.ElectionTick} ticks`],
                  ["Heartbeat Tick", `${spec.Raft.HeartbeatTick} ticks`],
                ]}
              />
            </div>
          }
          onOpen={() => {
            setDraftSnapshotInterval(spec.Raft.SnapshotInterval);
            setDraftLogEntries(spec.Raft.LogEntriesForSlowFollowers);
            setDraftKeepOldSnapshots(spec.Raft.KeepOldSnapshots ?? 0);
          }}
          onSave={async () => {
            await api.patchSwarmRaft({
              SnapshotInterval: draftSnapshotInterval,
              LogEntriesForSlowFollowers: draftLogEntries,
              KeepOldSnapshots: draftKeepOldSnapshots,
            });
            fetchSwarmInfo();
          }}
        />

        {/* CA Configuration */}
        <EditablePanel
          title="CA Configuration"
          requiredLevel={opsLevel.impactful}
          headerActions={
            <AlertDialog>
              <AlertDialogTrigger
                render={
                  <Button
                    variant="outline"
                    size="xs"
                    disabled={forceRotateCA.loading}
                  >
                    {forceRotateCA.loading ? (
                      <Spinner className="size-3" />
                    ) : (
                      <RefreshCw className="size-3" />
                    )}
                    Force Rotate
                  </Button>
                }
              />
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>Force CA certificate rotation?</AlertDialogTitle>
                  <AlertDialogDescription>
                    This will trigger an immediate rotation of all TLS certificates across the
                    cluster. All nodes will need to re-issue their certificates.
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>Cancel</AlertDialogCancel>
                  <AlertDialogAction
                    onClick={() =>
                      void forceRotateCA.execute(async () => {
                        await api.forceRotateCA();
                        fetchSwarmInfo();
                      }, "Failed to force CA rotation")
                    }
                  >
                    Rotate
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          }
          display={
            <div className="space-y-2">
              <KVTable
                rows={[
                  spec.CAConfig.NodeCertExpiry !== 0 && [
                    "Node Certificate Expiry",
                    formatDuration(spec.CAConfig.NodeCertExpiry),
                  ],
                  ["Force Rotate", String(spec.CAConfig.ForceRotate ?? 0)],
                  ["Root Rotation In Progress", swarm.RootRotationInProgress ? "Yes" : "No"],
                  ...(spec.CAConfig.ExternalCAs?.map(
                    ({ Protocol, URL }, index): [string, string] => [
                      `External CA ${index + 1}`,
                      `${Protocol} — ${URL}`,
                    ],
                  ) ?? []),
                ]}
              />
              {forceRotateCA.error && (
                <p className="text-xs text-red-600 dark:text-red-400">{forceRotateCA.error}</p>
              )}
            </div>
          }
          edit={
            <div className="space-y-3">
              <label className="block space-y-1">
                <span className="text-xs text-muted-foreground">Node Certificate Expiry</span>
                <DurationInput
                  value={draftCertExpiry}
                  onChange={setDraftCertExpiry}
                />
              </label>

              <KVTable
                rows={[
                  ["Force Rotate", String(spec.CAConfig.ForceRotate ?? 0)],
                  ["Root Rotation In Progress", swarm.RootRotationInProgress ? "Yes" : "No"],
                  ...(spec.CAConfig.ExternalCAs?.map(
                    ({ Protocol, URL }, index): [string, string] => [
                      `External CA ${index + 1}`,
                      `${Protocol} — ${URL}`,
                    ],
                  ) ?? []),
                ]}
              />
            </div>
          }
          onOpen={() => {
            setDraftCertExpiry(spec.CAConfig.NodeCertExpiry);
          }}
          onSave={async () => {
            await api.patchSwarmCAConfig({ NodeCertExpiry: draftCertExpiry });
            fetchSwarmInfo();
          }}
        />

        {/* Orchestration */}
        <EditablePanel
          title="Orchestration"
          requiredLevel={opsLevel.configuration}
          display={
            <KVTable
              rows={[
                [
                  "Task History Retention Limit",
                  spec.Orchestration.TaskHistoryRetentionLimit != null
                    ? String(spec.Orchestration.TaskHistoryRetentionLimit)
                    : "—",
                ],
              ]}
            />
          }
          edit={
            <NumberField
              value={draftTaskHistoryLimit || undefined}
              onChange={(next) => setDraftTaskHistoryLimit(next ?? 0)}
              min={0}
              step={1}
              label="Task History Retention Limit"
            />
          }
          onOpen={() => {
            setDraftTaskHistoryLimit(spec.Orchestration.TaskHistoryRetentionLimit ?? 0);
          }}
          onSave={async () => {
            await api.patchSwarmOrchestration({
              TaskHistoryRetentionLimit: draftTaskHistoryLimit,
            });
            fetchSwarmInfo();
          }}
        />

        {/* Dispatcher */}
        <EditablePanel
          title="Dispatcher"
          requiredLevel={opsLevel.configuration}
          display={
            <KVTable
              rows={[
                spec.Dispatcher.HeartbeatPeriod !== 0 && [
                  "Heartbeat Period",
                  formatDuration(spec.Dispatcher.HeartbeatPeriod),
                ],
              ]}
            />
          }
          edit={
            <label className="block space-y-1">
              <span className="text-xs text-muted-foreground">Heartbeat Period</span>
              <DurationInput
                value={draftHeartbeatPeriod}
                onChange={setDraftHeartbeatPeriod}
              />
            </label>
          }
          onOpen={() => {
            setDraftHeartbeatPeriod(spec.Dispatcher.HeartbeatPeriod);
          }}
          onSave={async () => {
            await api.patchSwarmDispatcher({ HeartbeatPeriod: draftHeartbeatPeriod });
            fetchSwarmInfo();
          }}
        />

        {/* Encryption */}
        <div className="space-y-3">
          <EditablePanel
            title="Encryption"
            requiredLevel={opsLevel.impactful}
            display={
              <KVTable
                rows={[
                  ["Auto-Lock Managers", spec.EncryptionConfig.AutoLockManagers ? "Yes" : "No"],
                ]}
              />
            }
            edit={
              <label className="flex items-center gap-3 text-sm">
                <Switch
                  checked={draftAutoLock}
                  onCheckedChange={setDraftAutoLock}
                />
                Auto-Lock Managers
              </label>
            }
            onOpen={() => {
              setDraftAutoLock(spec.EncryptionConfig.AutoLockManagers);
            }}
            onSave={async () => {
              await api.patchSwarmEncryption({ AutoLockManagers: draftAutoLock });
              fetchSwarmInfo();
            }}
          />

          {spec.EncryptionConfig.AutoLockManagers && (
            <div className="flex flex-wrap items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={fetchUnlockKey.loading}
                onClick={() => {
                  if (showUnlockKey) {
                    setShowUnlockKey(false);
                    setUnlockKeyValue(null);
                  } else {
                    void fetchUnlockKey.execute(async () => {
                      const result = await api.unlockKey();
                      setUnlockKeyValue(result.unlockKey);
                      setShowUnlockKey(true);
                    }, "Failed to fetch unlock key");
                  }
                }}
              >
                {fetchUnlockKey.loading ? (
                  <Spinner className="size-3" />
                ) : (
                  <KeyRound className="size-3.5" />
                )}
                {showUnlockKey ? "Hide Unlock Key" : "Show Unlock Key"}
              </Button>

              <AlertDialog>
                <AlertDialogTrigger
                  render={
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={rotateUnlockKey.loading}
                    >
                      {rotateUnlockKey.loading ? (
                        <Spinner className="size-3" />
                      ) : (
                        <RefreshCw className="size-3.5" />
                      )}
                      Rotate Unlock Key
                    </Button>
                  }
                />
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>Rotate unlock key?</AlertDialogTitle>
                    <AlertDialogDescription>
                      This will invalidate the current unlock key. Make sure to save the new key.
                    </AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogCancel>Cancel</AlertDialogCancel>
                    <AlertDialogAction
                      onClick={() =>
                        void rotateUnlockKey.execute(async () => {
                          await api.rotateUnlockKey();
                          setShowUnlockKey(false);
                          setUnlockKeyValue(null);
                          fetchSwarmInfo();
                        }, "Failed to rotate unlock key")
                      }
                    >
                      Rotate
                    </AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>

              <Dialog
                open={unlockOpen}
                onOpenChange={(open) => {
                  setUnlockOpen(open);

                  if (!open) {
                    setUnlockInput("");
                  }
                }}
              >
                <DialogTrigger
                  render={
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={unlockSwarm.loading}
                    >
                      {unlockSwarm.loading ? (
                        <Spinner className="size-3" />
                      ) : (
                        <LockOpen className="size-3.5" />
                      )}
                      Unlock Swarm
                    </Button>
                  }
                />
                <DialogContent>
                  <DialogHeader>
                    <DialogTitle>Unlock swarm</DialogTitle>
                    <DialogDescription>
                      Enter the unlock key to unlock a locked swarm manager.
                    </DialogDescription>
                  </DialogHeader>
                  <Input
                    placeholder="SWMKEY-1-..."
                    value={unlockInput}
                    onChange={(event) => setUnlockInput(event.target.value)}
                    className="font-mono"
                  />
                  {unlockSwarm.error && (
                    <p className="text-xs text-red-600 dark:text-red-400">{unlockSwarm.error}</p>
                  )}
                  <DialogFooter>
                    <Button
                      disabled={unlockSwarm.loading || !unlockInput.trim()}
                      onClick={() =>
                        void unlockSwarm.execute(async () => {
                          await api.unlockSwarm(unlockInput.trim());
                          setUnlockOpen(false);
                          setUnlockInput("");
                          fetchSwarmInfo();
                        }, "Failed to unlock swarm")
                      }
                    >
                      {unlockSwarm.loading ? <Spinner className="size-3" /> : null}
                      Unlock
                    </Button>
                  </DialogFooter>
                </DialogContent>
              </Dialog>
            </div>
          )}

          {fetchUnlockKey.error && (
            <p className="text-xs text-red-600 dark:text-red-400">{fetchUnlockKey.error}</p>
          )}

          {rotateUnlockKey.error && (
            <p className="text-xs text-red-600 dark:text-red-400">{rotateUnlockKey.error}</p>
          )}

          {showUnlockKey && unlockKeyValue && (
            <div className="space-y-2">
              <pre className="rounded-lg bg-muted/50 p-3 font-mono text-xs select-all">
                {unlockKeyValue}
              </pre>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  navigator.clipboard.writeText(unlockKeyValue).then(() => {
                    setUnlockKeyCopied(true);
                    setTimeout(() => setUnlockKeyCopied(false), 2000);
                  });
                }}
              >
                {unlockKeyCopied ? (
                  <>
                    <Check className="size-3" />
                    Copied
                  </>
                ) : (
                  <>
                    <Copy className="size-3" />
                    Copy
                  </>
                )}
              </Button>
            </div>
          )}
        </div>

        {/* Task Defaults */}
        {spec.TaskDefaults.LogDriver && (
          <CollapsibleSection title="Task Defaults">
            <KVTable
              rows={[
                ["Log Driver", spec.TaskDefaults.LogDriver.Name],
                ...(spec.TaskDefaults.LogDriver.Options
                  ? Object.entries(spec.TaskDefaults.LogDriver.Options).map(
                      ([key, value]): [string, string] => [`Log Driver: ${key}`, value],
                    )
                  : []),
              ]}
            />
          </CollapsibleSection>
        )}
      </div>

      {/* Plugins */}
      <CollapsibleSection
        title="Plugins"
        controls={
          level >= opsLevel.impactful ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => setInstallOpen(true)}
            >
              <Plus className="size-3.5" />
              Install Plugin
            </Button>
          ) : undefined
        }
      >
        {plugins.length === 0 ? (
          <p className="py-4 text-sm text-muted-foreground">No plugins installed.</p>
        ) : (
          <div className="space-y-3">
            <PluginTable plugins={plugins} />
          </div>
        )}
      </CollapsibleSection>

      <InstallPluginDialog
        open={installOpen}
        onOpenChange={setInstallOpen}
        onInstalled={() => {
          api
            .plugins()
            .then(setPlugins)
            .catch(() => {});
        }}
      />
    </div>
  );
}
