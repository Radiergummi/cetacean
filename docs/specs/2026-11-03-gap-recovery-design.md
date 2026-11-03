# Recovering what changed during a gap — design

Follows the open point in [the timeline store design](2026-10-26-timeline-store-design.md#open):
after a restart, the history marks the time Cetacean wasn't watching as a gap, and says nothing
about what changed in it. The Docker daemon still remembers some of it.

## What the daemon has

The engine keeps its last 256 events in memory, of every type, and replays them to a subscription
with `since`, ending at `until`. That buffer:

- is shared with container events, so on a busy manager it spans minutes, not hours;
- is lost when the daemon restarts, and is per daemon;
- holds no task events: Swarm emits none. Task changes during a gap stay unknown.

So a recovery can name the service, node, config, secret, network and volume changes made during
the most recent stretch of the gap. It can never close a gap: tasks are missing from all of it.

## Proposal

**Read once, before the stream records anything.** The watcher calls a hook once a sync has first
succeeded and before the event stream beside it records a change, giving the time that stream
replays from. The timeline writer records the gap there: it reads the previous process's last
heartbeat, then reads the daemon's whole buffer (`/events` from the epoch to the stream's start,
every type) with a short timeout. Coverage opens only after the hook. A stream already running
when a sync first succeeds, or a store that fails at that moment, records the gap later, with no
recovery: anything past-dated would then sort above changes already watched.

**The bounds.** Recovery takes the events after `from` plus the watcher's replay margin, which the
previous process may already have recorded by the daemon's clock, and before the stream's start,
which the stream replays itself. Both bounds compare the daemon's own timestamps, so neither side
of the stream's start is read twice. The recovered changes and the gap are appended before the
stream records anything, so IDs and times still rise together and a time-bounded walk can stop at
the first entry that ended before its window.

**Each event becomes a change entry,** stamped with the event's time and carrying its type, action
(`create`, `update`, `remove`), resource ID and the name from its attributes. Nothing is re-read:
the resource's state at that moment is gone, and the first sync already holds its current one. It
has no actor, since attribution lived in the process that stopped. A service's entry takes its
stack from the snapshot loaded before the first sync, which still holds a service removed in the
gap. It is marked `recovered: true` on every representation — JSON, CSV, the feeds, `get_events`
— and the dashboard says "recovered from Docker's event log" on its row. A recovered change never
runs alone with a watched one, and SSE replay skips it: it was never live.

**The gap stays, and says how far recovery reached.** It gains `recoveredFrom`: the time from which
those six types are complete, which is the oldest buffered event when that falls after the start
above, and that start itself when the buffer reaches across it. The oldest event is the bound even
when the buffer is not full: a daemon restarted during the gap kept nothing from before it, and
nothing tells the two cases apart. The divider reads "Not watching for 1h, partly covered by
Docker's log", with the time in its title; the feed entry's title says the same.

## Not in this design

- Recovering task changes, from container events or otherwise: containers on other nodes report
  only to their own engine.
- Reading other managers' buffers.
- A summary of what an update changed: the event says which resource changed, not how.

## Decisions

Settled on 2026-11-03:

1. **Recovered entries are marked**, in every representation, rather than presented as watched.
2. **The gap stays**, with `recoveredFrom`, rather than shrinking: a narrower gap would claim the
   recovered stretch was watched, and its task changes were not.
