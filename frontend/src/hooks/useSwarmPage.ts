import { api, emptyMethods } from "../api/client";
import type { Plugin, SwarmInfo } from "../api/types";
import { useCallback, useEffect, useState } from "react";

export function useSwarmPage() {
  const [data, setData] = useState<SwarmInfo | null>(null);
  const [plugins, setPlugins] = useState<Plugin[]>([]);
  const [error, setError] = useState(false);
  const [allowedMethods, setAllowedMethods] = useState(emptyMethods);

  const fetchSwarmInfo = useCallback(() => {
    api
      .swarm()
      .then(({ data: swarmData, allowedMethods: methods }) => {
        setData(swarmData);
        setAllowedMethods(methods);
      })
      .catch(() => setError(true));
  }, []);

  const refetchPlugins = useCallback(() => {
    api
      .plugins()
      .then(({ data: pluginsData }) => setPlugins(pluginsData))
      .catch(console.warn);
  }, []);

  useEffect(() => {
    fetchSwarmInfo();
    refetchPlugins();
  }, [fetchSwarmInfo, refetchPlugins]);

  // Orchestration draft
  const [draftTaskHistoryLimit, setDraftTaskHistoryLimit] = useState(0);

  // Raft draft
  const [draftSnapshotInterval, setDraftSnapshotInterval] = useState(0);
  const [draftLogEntries, setDraftLogEntries] = useState(0);
  const [draftKeepOldSnapshots, setDraftKeepOldSnapshots] = useState(0);

  // Dispatcher draft
  const [draftHeartbeatPeriod, setDraftHeartbeatPeriod] = useState(0);

  return {
    data,
    plugins,
    error,
    allowedMethods,
    fetchSwarmInfo,
    refetchPlugins,
    draftTaskHistoryLimit,
    setDraftTaskHistoryLimit,
    draftSnapshotInterval,
    setDraftSnapshotInterval,
    draftLogEntries,
    setDraftLogEntries,
    draftKeepOldSnapshots,
    setDraftKeepOldSnapshots,
    draftHeartbeatPeriod,
    setDraftHeartbeatPeriod,
  };
}
