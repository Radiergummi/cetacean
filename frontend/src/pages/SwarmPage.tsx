import { api } from "../api/client";
import type { Plugin, SwarmInfo } from "../api/types";
import CollapsibleSection from "../components/CollapsibleSection";
import { KVTable, MetadataGrid, ResourceId, Timestamp } from "../components/data";
import FetchError from "../components/FetchError";
import InfoCard from "../components/InfoCard";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
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
import { formatDuration } from "../lib/format";
import { Check, Copy } from "lucide-react";
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

  const fetchData = useCallback(() => {
    api
      .swarm()
      .then(setData)
      .catch(() => setError(true));
    api
      .plugins()
      .then(setPlugins)
      .catch(() => {});
  }, []);

  useEffect(fetchData, [fetchData]);

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
        <CollapsibleSection title="Raft">
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
        </CollapsibleSection>

        {/* CA Configuration */}
        <CollapsibleSection title="CA Configuration">
          <KVTable
            rows={[
              spec.CAConfig.NodeCertExpiry !== 0 && [
                "Node Certificate Expiry",
                formatDuration(spec.CAConfig.NodeCertExpiry),
              ],
              ["Force Rotate", String(spec.CAConfig.ForceRotate)],
              ["Root Rotation In Progress", swarm.RootRotationInProgress ? "Yes" : "No"],
              ...(spec.CAConfig.ExternalCAs?.map(({ Protocol, URL }, index): [string, string] => [
                `External CA ${index + 1}`,
                `${Protocol} — ${URL}`,
              ]) ?? []),
            ]}
          />
        </CollapsibleSection>

        {/* Orchestration */}
        <CollapsibleSection title="Orchestration">
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
        </CollapsibleSection>

        {/* Dispatcher */}
        <CollapsibleSection title="Dispatcher">
          <KVTable
            rows={[
              spec.Dispatcher.HeartbeatPeriod !== 0 && [
                "Heartbeat Period",
                formatDuration(spec.Dispatcher.HeartbeatPeriod),
              ],
            ]}
          />
        </CollapsibleSection>

        {/* Encryption */}
        <CollapsibleSection title="Encryption">
          <KVTable
            rows={[["Auto-Lock Managers", spec.EncryptionConfig.AutoLockManagers ? "Yes" : "No"]]}
          />
        </CollapsibleSection>

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
      {plugins.length > 0 && (
        <CollapsibleSection title="Plugins">
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
                    <td className="p-3 font-mono text-xs">{Name}</td>
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
        </CollapsibleSection>
      )}
    </div>
  );
}
