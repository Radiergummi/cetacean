import { Spinner } from "../Spinner";
import { ArrowDown, ArrowUp } from "lucide-react";

interface LogOverlaysProps {
  atTop: boolean;
  hasOlderLogs: boolean;
  loadOlder: () => void;
  loadingOlder: boolean;
  pinnedCount: number;
  following: boolean;
  setFollowing: (value: boolean) => void;
  live: boolean;
  hasNewerLogs: boolean;
  loadNewer: () => void;
  loadingNewer: boolean;
}

export function LogOverlays({
  atTop,
  hasOlderLogs,
  loadOlder,
  loadingOlder,
  pinnedCount,
  following,
  setFollowing,
  live,
  hasNewerLogs,
  loadNewer,
  loadingNewer,
}: LogOverlaysProps) {
  return (
    <>
      {atTop && hasOlderLogs && (
        <button
          onClick={loadOlder}
          disabled={loadingOlder}
          data-pinned={pinnedCount || undefined}
          className="absolute top-3 left-1/2 flex -translate-x-1/2 items-center gap-1.5 rounded-full border bg-card px-3 py-1.5 text-xs text-foreground shadow-lg transition-colors hover:bg-muted data-[pinned='1']:top-8 data-[pinned='2']:top-13 data-[pinned='3']:top-18"
        >
          {loadingOlder ? <Spinner className="size-3" /> : <ArrowUp className="size-3" />}
          Load older
        </button>
      )}

      {!following ? (
        <button
          onClick={() => setFollowing(true)}
          className="absolute bottom-3 left-1/2 flex -translate-x-1/2 items-center gap-1.5 rounded-full border bg-card px-3 py-1.5 text-xs text-foreground shadow-lg transition-colors hover:bg-muted"
        >
          <ArrowDown className="size-3" />
          Jump to bottom
        </button>
      ) : !live && hasNewerLogs ? (
        <button
          onClick={loadNewer}
          disabled={loadingNewer}
          className="absolute bottom-3 left-1/2 flex -translate-x-1/2 items-center gap-1.5 rounded-full border bg-card px-3 py-1.5 text-xs text-foreground shadow-lg transition-colors hover:bg-muted"
        >
          Load newer
          {loadingNewer ? <Spinner className="size-3" /> : <ArrowDown className="size-3" />}
        </button>
      ) : null}
    </>
  );
}
