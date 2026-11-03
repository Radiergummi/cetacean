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

**Read once, at ready, before the gap is recorded.** When the writer is about to record a gap from
`from` to now, it first asks the daemon for `/events?since=from&until=now`, filtered to the six
types above, and separately for the oldest buffered event of any type. The read is bounded by the
256-entry buffer and a short timeout; a failure records the gap as today.

**Each event becomes a change entry,** stamped with the event's time and carrying its type, action
(`create`, `update`, `remove`), resource ID and the name from its attributes. Nothing is re-read:
the resource's state at that moment is gone, and the first sync already holds its current one. It
has no actor, since attribution lived in the process that stopped. It is marked `recovered: true`
on every representation — JSON, CSV, the feeds, `get_events` — and the dashboard says "recovered
from Docker's event log" on its row.

**The gap stays, and says how far recovery reached.** It gains `recoveredFrom`: the time from which
those six types are complete, which is the oldest buffered event when that falls after `from`, and
`from` itself when the buffer reaches across the whole gap. The divider reads "Cetacean wasn't
watching from 10:00 to 11:00; changes to services, nodes, configs, secrets, networks and volumes
since 10:40 were recovered. Task changes are unknown." `get_events`' description says the same.

**Order.** The recovered entries are appended just before the gap, so they take the IDs between the
last entry before it and the gap itself. A newest-first list then shows the divider, the recovered
changes below it, then what came before — the divider heads its recovered changes. A time-ordered
view (the charts' markers, `get_events`) places them inside the gap by their timestamps. They are
appended to the history under `notifyMu` without being dispatched as live events: they describe the
past, and the gap's own event already tells every client to refetch its timeline.

**Replays are not duplicated.** Events the live stream replays through `eventsSince`'s margin
arrive after ready and are recorded as today; recovery stops at the moment the gap is recorded.

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
