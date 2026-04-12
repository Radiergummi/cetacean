import { api } from "../api/client";
import CollapsibleSection from "../components/CollapsibleSection";
import { KVTable, MetadataGrid, ResourceId, Timestamp } from "../components/data";
import FetchError from "../components/FetchError";
import InfoCard from "../components/InfoCard";
import InstallPluginDialog from "../components/InstallPluginDialog";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import PluginTable from "../components/PluginTable";
import { CAConfigPanel } from "../components/swarm-detail/CAConfigPanel";
import { EncryptionPanel } from "../components/swarm-detail/EncryptionPanel";
import { SwarmActions } from "../components/swarm-detail/SwarmActions";
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
import { useSwarmPage } from "../hooks/useSwarmPage";
import { formatDuration } from "../lib/format";
import { EditablePanel } from "@/components/service-detail/EditablePanel";
import { Check, Copy, Plus } from "lucide-react";
import { useState } from "react";

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
  const page = useSwarmPage();
  const [installOpen, setInstallOpen] = useState(false);

  if (page.error) {
    return <FetchError message="Failed to load swarm info" />;
  }

  if (!page.data) {
    return <LoadingDetail />;
  }

  const { swarm, managerAddr } = page.data;
  const spec = swarm.Spec;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Swarm"
        actions={
          <>
            <SwarmActions
              allowedMethods={page.allowedMethods}
              onRotated={page.fetchSwarmInfo}
            />
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
          canEdit={page.allowedMethods.has("PATCH")}
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
                value={page.draftSnapshotInterval || undefined}
                onChange={(next) => page.setDraftSnapshotInterval(next ?? 0)}
                min={0}
                step={1000}
                label="Snapshot Interval"
              />

              <NumberField
                value={page.draftKeepOldSnapshots || undefined}
                onChange={(next) => page.setDraftKeepOldSnapshots(next ?? 0)}
                min={0}
                step={1}
                label="Keep Old Snapshots"
              />

              <NumberField
                value={page.draftLogEntries || undefined}
                onChange={(next) => page.setDraftLogEntries(next ?? 0)}
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
            page.setDraftSnapshotInterval(spec.Raft.SnapshotInterval);
            page.setDraftLogEntries(spec.Raft.LogEntriesForSlowFollowers);
            page.setDraftKeepOldSnapshots(spec.Raft.KeepOldSnapshots ?? 0);
          }}
          onSave={async () => {
            await api.patchSwarmRaft({
              SnapshotInterval: page.draftSnapshotInterval,
              LogEntriesForSlowFollowers: page.draftLogEntries,
              KeepOldSnapshots: page.draftKeepOldSnapshots,
            });
            page.fetchSwarmInfo();
          }}
        />

        {/* CA Configuration */}
        <CAConfigPanel
          spec={spec}
          rootRotationInProgress={swarm.RootRotationInProgress}
          canEdit={page.allowedMethods.has("POST")}
          onSaved={page.fetchSwarmInfo}
        />

        {/* Orchestration */}
        <EditablePanel
          title="Orchestration"
          canEdit={page.allowedMethods.has("PATCH")}
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
              value={page.draftTaskHistoryLimit || undefined}
              onChange={(next) => page.setDraftTaskHistoryLimit(next ?? 0)}
              min={0}
              step={1}
              label="Task History Retention Limit"
            />
          }
          onOpen={() => {
            page.setDraftTaskHistoryLimit(spec.Orchestration.TaskHistoryRetentionLimit ?? 0);
          }}
          onSave={async () => {
            await api.patchSwarmOrchestration({
              TaskHistoryRetentionLimit: page.draftTaskHistoryLimit,
            });
            page.fetchSwarmInfo();
          }}
        />

        {/* Dispatcher */}
        <EditablePanel
          title="Dispatcher"
          canEdit={page.allowedMethods.has("PATCH")}
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
                value={page.draftHeartbeatPeriod}
                onChange={page.setDraftHeartbeatPeriod}
              />
            </label>
          }
          onOpen={() => {
            page.setDraftHeartbeatPeriod(spec.Dispatcher.HeartbeatPeriod);
          }}
          onSave={async () => {
            await api.patchSwarmDispatcher({ HeartbeatPeriod: page.draftHeartbeatPeriod });
            page.fetchSwarmInfo();
          }}
        />

        {/* Encryption */}
        <EncryptionPanel
          spec={spec}
          canEdit={page.allowedMethods.has("POST")}
          onSaved={page.fetchSwarmInfo}
        />

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
          page.allowedMethods.has("POST") ? (
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
        {page.plugins.length === 0 ? (
          <p className="py-4 text-sm text-muted-foreground">No plugins installed.</p>
        ) : (
          <div className="space-y-3">
            <PluginTable plugins={page.plugins} />
          </div>
        )}
      </CollapsibleSection>

      <InstallPluginDialog
        open={installOpen}
        onOpenChange={setInstallOpen}
        onInstalled={page.refetchPlugins}
      />
    </div>
  );
}
