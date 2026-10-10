import { api } from "@/api/client";
import { Spinner } from "@/components/Spinner";
import { useQuery } from "@tanstack/react-query";
import { TriangleAlert } from "lucide-react";

function services(count: number): string {
  return count === 1 ? "1 service" : `${count} services`;
}

/**
 * What draining a node would do, from `GET /nodes/{id}/drain-impact`: the
 * same assessment MCP's drain-impact view gives. Fetched only while the
 * confirmation is open, and afresh each time, since placement moves on.
 */
export function DrainImpactSummary({ nodeId, enabled }: { nodeId: string; enabled: boolean }) {
  const { data, isError, isPending } = useQuery({
    queryKey: ["drain-impact", nodeId],
    queryFn: ({ signal }) => api.nodeDrainImpact(nodeId, signal),
    enabled,
    staleTime: 0,
    gcTime: 0,
  });

  if (isError) {
    return (
      <p className="text-sm text-muted-foreground">
        Could not check where this node's tasks can go. Swarm reschedules them only onto nodes their
        placement constraints allow; any that fit nowhere stay pending.
      </p>
    );
  }

  if (isPending) {
    return (
      <p className="flex items-center gap-2 text-sm text-muted-foreground">
        <Spinner className="size-3" />
        Checking where this node's tasks can go…
      </p>
    );
  }

  const affected = data.nodes.filter(({ type }) => type === "service");
  const movable = affected.filter(({ state }) => state === "movable");
  const stranded = affected.filter(({ state }) => state === "stranded");
  const global = affected.filter(({ state }) => state === "global");

  return (
    <div className="space-y-2 text-sm">
      {affected.length === 0 && (
        <p className="text-muted-foreground">No service has tasks on this node.</p>
      )}

      {movable.length > 0 && <p>{services(movable.length)} will move to other nodes.</p>}

      {global.length > 0 && (
        <p className="text-muted-foreground">
          {global.length === 1 ? "1 global service stops" : `${global.length} global services stop`}{" "}
          on this node.
        </p>
      )}

      {stranded.length > 0 && (
        <div className="rounded-md border border-status-warning/40 bg-status-warning/10 p-2.5">
          <p className="flex items-center gap-1.5 font-medium text-status-warning">
            <TriangleAlert className="size-3.5 shrink-0" />
            {services(stranded.length)} can't run on any other node and will sit pending:
          </p>
          <ul className="mt-1.5 space-y-1">
            {stranded.map(({ id, label, detail }) => (
              <li key={id}>
                <span className="font-medium">{label}</span>
                {detail && (
                  <>
                    {" — "}
                    <span className="font-mono text-xs text-muted-foreground">{detail}</span>
                  </>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}

      {data.note && <p className="text-xs text-muted-foreground">{data.note}</p>}
    </div>
  );
}
