import {Check, Copy} from "lucide-react";
import {useCallback, useEffect, useState} from "react";
import {api} from "../api/client";
import type {Plugin, SwarmInfo} from "../api/types";
import {KVTable, ResourceId, SectionHeader, Timestamp} from "../components/data";
import {formatNs} from "../lib/formatNs";
import FetchError from "../components/FetchError";
import InfoCard from "../components/InfoCard";
import {LoadingDetail} from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import {Button} from "../components/ui/button";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
    DialogTrigger,
} from "../components/ui/dialog";


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
            <DialogTrigger render={<Button variant={variant} size="sm"/>}>
                Join {label}
            </DialogTrigger>
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>Join as {label}</DialogTitle>
                    <DialogDescription>
                        Run this command on the node you want to join to the swarm.
                    </DialogDescription>
                </DialogHeader>
                <pre className="text-xs font-mono leading-normal bg-muted/50 rounded-lg p-3 break-all select-all wrap-anywhere max-w-full whitespace-normal">
                    {joinCmd}
                </pre>
                <DialogFooter>
                    <Button variant="ghost" onClick={copyToClipboard}>
                        {copied ? (
                            <>
                                <Check data-icon="inline-start"/>
                                Copied
                            </>
                        ) : (
                            <>
                                <Copy data-icon="inline-start"/>
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
        api.plugins().then(setPlugins).catch(() => {
        });
    }, []);

    useEffect(fetchData, [fetchData]);

    if (error) {
        return <FetchError message="Failed to load swarm info"/>;
    }
    if (!data) {
        return <LoadingDetail/>;
    }

    const {swarm, managerAddr} = data;
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
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                <ResourceId label="Cluster ID" id={swarm.ID}/>
                <Timestamp label="Created" date={swarm.CreatedAt}/>
                <Timestamp label="Updated" date={swarm.UpdatedAt}/>
                <InfoCard label="Default Address Pool" value={swarm.DefaultAddrPool?.join(", ") || "—"}/>
                <InfoCard label="Subnet Size" value={swarm.SubnetSize ? `/${swarm.SubnetSize}` : "—"}/>
                <InfoCard label="Data Path Port" value={swarm.DataPathPort ? String(swarm.DataPathPort) : "—"}/>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
                {/* Raft */}
                <section>
                    <SectionHeader title="Raft"/>
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
                </section>

                {/* CA Configuration */}
                <section>
                    <SectionHeader title="CA Configuration"/>
                    <KVTable
                        rows={[
                            spec.CAConfig.NodeCertExpiry !== 0 && [
                                "Node Certificate Expiry",
                                formatNs(spec.CAConfig.NodeCertExpiry),
                            ],
                            ["Force Rotate", String(spec.CAConfig.ForceRotate)],
                            ["Root Rotation In Progress", swarm.RootRotationInProgress ? "Yes" : "No"],
                            ...(
                                spec.CAConfig.ExternalCAs?.map(({Protocol, URL}, index): [string, string] => [
                                    `External CA ${index + 1}`,
                                    `${Protocol} — ${URL}`,
                                ]) ?? []
                            ),
                        ]}
                    />
                </section>

                {/* Orchestration */}
                <section>
                    <SectionHeader title="Orchestration"/>
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
                </section>

                {/* Dispatcher */}
                <section>
                    <SectionHeader title="Dispatcher"/>
                    <KVTable
                        rows={[
                            spec.Dispatcher.HeartbeatPeriod !== 0 && [
                                "Heartbeat Period",
                                formatNs(spec.Dispatcher.HeartbeatPeriod),
                            ],
                        ]}
                    />
                </section>

                {/* Encryption */}
                <section>
                    <SectionHeader title="Encryption"/>
                    <KVTable
                        rows={[["Auto-Lock Managers", spec.EncryptionConfig.AutoLockManagers ? "Yes" : "No"]]}
                    />
                </section>

                {/* Task Defaults */}
                {spec.TaskDefaults.LogDriver && (
                    <section>
                        <SectionHeader title="Task Defaults"/>
                        <KVTable
                            rows={[
                                ["Log Driver", spec.TaskDefaults.LogDriver.Name],
                                ...(
                                    spec.TaskDefaults.LogDriver.Options
                                        ? Object.entries(spec.TaskDefaults.LogDriver.Options).map(
                                            ([key, value]): [string, string] => [`Log Driver: ${key}`, value],
                                        )
                                        : []
                                ),
                            ]}
                        />
                    </section>
                )}
            </div>

            {/* Plugins */}
            {plugins.length > 0 && (
                <section>
                    <SectionHeader title="Plugins"/>
                    <div className="overflow-x-auto rounded-lg border">
                        <table className="w-full">
                            <thead>
                            <tr className="border-b text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                                <th className="p-3">Name</th>
                                <th className="p-3">Type</th>
                                <th className="p-3">Status</th>
                            </tr>
                            </thead>
                            <tbody>
                            {plugins.map(({Config: {Interface}, Enabled, Id, Name}) => (
                                <tr key={Id ?? Name} className="border-b last:border-b-0">
                                    <td className="p-3 font-mono text-xs">{Name}</td>
                                    <td className="p-3 text-sm text-muted-foreground">
                                        {Interface.Types.map(({Capability}) => Capability).join(", ") || "—"}
                                    </td>
                                    <td className="p-3">
                                        <span
                                            data-enabled={Enabled || undefined}
                                            className="inline-flex items-center rounded-full px-2 py-0.5 text-xs
                                            font-medium bg-muted text-muted-foreground data-enabled:bg-green-500/10
                                            data-enabled:text-green-500"
                                        >
                                            {Enabled ? "Enabled" : "Disabled"}
                                        </span>
                                    </td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                </section>
            )}
        </div>
    );
}
