import type { LogLine } from "../../api/client";

const maxClockSkewMilliseconds = 60_000;

/**
 * The last line shown from each task. Every task's timestamps come from its
 * own node's clock, so a resume starts from the task furthest behind and
 * replays what the others already showed; this is what recognises those.
 */
export class TaskPositions {
  private readonly last = new Map<string, { timestamp: string; message: string }>();

  record({ timestamp, message, attrs }: LogLine): void {
    if (!timestamp) {
      return;
    }

    const task = attrs?.["taskId"] ?? "";
    const previous = this.last.get(task);

    if (!previous || timestamp >= previous.timestamp) {
      this.last.set(task, { timestamp, message });
    }
  }

  isReplayed({ timestamp, message, attrs }: LogLine): boolean {
    const previous = timestamp ? this.last.get(attrs?.["taskId"] ?? "") : undefined;

    if (!previous) {
      return false;
    }

    return (
      timestamp < previous.timestamp ||
      (timestamp === previous.timestamp && message === previous.message)
    );
  }

  /** The server's resume rule: the oldest task, at most a minute behind the newest. */
  resumeCursor(): string | undefined {
    const timestamps = [...this.last.values()].map(({ timestamp }) => timestamp).sort();
    const oldest = timestamps[0];
    const newest = timestamps.at(-1);

    if (oldest === undefined || newest === undefined) {
      return undefined;
    }

    const bound = new Date(Date.parse(newest) - maxClockSkewMilliseconds)
      .toISOString()
      .replace("Z", "000000Z");

    return oldest < bound ? bound : oldest;
  }

  clear(): void {
    this.last.clear();
  }
}
