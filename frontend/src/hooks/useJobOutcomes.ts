import { api, pageSize } from "../api/client";
import type { Service } from "../api/types";
import type { JobOutcome } from "../lib/serviceHealth";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

/** Invalidating this prefix refreshes every job outcome on the page. */
export const jobOutcomesKey = ["job-outcomes"] as const;

const finishedStates = ["complete", "failed", "rejected"] as const;

const noOutcomes: Record<string, JobOutcome> = {};

/**
 * How the finished tasks of the given jobs' current runs ended, by service ID.
 * Batched like a page of rows, since each filter travels in a GET's URL.
 */
export function useJobOutcomes(services: readonly Pick<Service, "ID" | "Spec" | "JobStatus">[]) {
  // Each job's current run, which keys the query: a re-run asks again.
  const iterations = useMemo(
    () =>
      services
        .filter(({ Spec: { Mode } }) => Mode.ReplicatedJob || Mode.GlobalJob)
        .map(({ ID, JobStatus }) => [ID, JobStatus?.JobIteration.Index] as const)
        .sort(([left], [right]) => left.localeCompare(right)),
    [services],
  );

  const { data } = useQuery({
    queryKey: [...jobOutcomesKey, ...iterations],
    queryFn: async ({ signal }) => {
      const current = new Map(iterations);
      const jobIds = [...current.keys()];
      const batches: string[][] = [];

      for (let offset = 0; offset < jobIds.length; offset += pageSize) {
        batches.push(jobIds.slice(offset, offset + pageSize));
      }

      const tasks = (
        await Promise.all(
          batches.map((batch) =>
            api.allTasks(
              {
                filter: `service in ${JSON.stringify(batch)} && state in ${JSON.stringify(finishedStates)}`,
              },
              signal,
            ),
          ),
        )
      ).flat();
      const outcomes: Record<string, JobOutcome> = {};

      for (const { ServiceID, JobIteration, Status } of tasks) {
        const iteration = current.get(ServiceID);

        // Swarm keeps earlier runs' tasks. Where either side lacks an
        // iteration, count the task, as the server's completion count does.
        if (
          iteration !== undefined &&
          JobIteration?.Index !== undefined &&
          JobIteration.Index !== iteration
        ) {
          continue;
        }

        const outcome = (outcomes[ServiceID] ??= { completed: 0, failed: 0 });

        if (Status.State === "complete") {
          outcome.completed++;
        } else {
          outcome.failed++;
        }
      }

      return outcomes;
    },
    enabled: iterations.length > 0,
  });

  return data ?? noOutcomes;
}
