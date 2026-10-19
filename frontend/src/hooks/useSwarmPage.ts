import { api, emptyMethods } from "../api/client";
import type { Plugin } from "../api/types";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useState } from "react";

const emptyPlugins: Plugin[] = [];

export function useSwarmPage() {
  const queryClient = useQueryClient();
  const swarmQuery = useQuery({ queryKey: ["swarm"], queryFn: () => api.swarm() });
  const pluginsQuery = useQuery({ queryKey: ["plugins"], queryFn: () => api.plugins() });

  const data = swarmQuery.data?.data ?? null;
  const editable = swarmQuery.data?.editable ?? emptyMethods;
  const canInstallPlugins = pluginsQuery.data?.allowedMethods.has("POST") ?? false;
  const error = swarmQuery.isError;
  const plugins = pluginsQuery.data?.data ?? emptyPlugins;

  const fetchSwarmInfo = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: ["swarm"] });
  }, [queryClient]);

  const refetchPlugins = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: ["plugins"] });
  }, [queryClient]);

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
    editable,
    canInstallPlugins,
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
