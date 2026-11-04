package docker

import (
	"context"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/events"

	"github.com/radiergummi/cetacean/internal/cache"
)

// recoverTimeout bounds the read of the daemon's buffer, which holds back the
// event stream once, as the gap it ends is recorded.
const recoverTimeout = 5 * time.Second

// recoverable are the event types the history records that the daemon keeps;
// Swarm emits none for tasks.
var recoverable = map[events.Type]cache.EventType{
	events.ServiceEventType: cache.EventService,
	events.NodeEventType:    cache.EventNode,
	events.SecretEventType:  cache.EventSecret,
	events.ConfigEventType:  cache.EventConfig,
	events.NetworkEventType: cache.EventNetwork,
	events.VolumeEventType:  cache.EventVolume,
}

// historyAction is the action history records for a resource event: Docker
// says destroy for a network or a volume where it says remove for the rest.
func historyAction(action events.Action) string {
	if action == "destroy" {
		return "remove"
	}

	return string(action)
}

// EventBuffer reads what a daemon still holds of its recent events.
type EventBuffer interface {
	BufferedEvents(ctx context.Context, until time.Time) ([]events.Message, error)
}

// RecoverGap reads what the daemon still holds of the gap from from until the
// live stream's start, which replays everything after it; serviceStacks names
// the stacks of the services from before the first sync forgot the removed
// ones. A failed read recovers nothing, and the gap is recorded as it was.
func RecoverGap(
	ctx context.Context,
	buffer EventBuffer,
	from, until time.Time,
	serviceStacks map[string]string,
) cache.Recovery {
	ctx, cancel := context.WithTimeout(ctx, recoverTimeout)
	defer cancel()

	buffered, err := buffer.BufferedEvents(ctx, until)
	if err != nil {
		slog.WarnContext(ctx, "could not read the changes made during the gap", "error", err)
		return cache.Recovery{}
	}

	return recoverFrom(buffered, from, until, serviceStacks)
}

// recoverFrom turns the buffered events inside the gap into recovered
// changes. It starts a replay margin after from, which the previous process
// may have recorded by the daemon's clock, and is complete from there when
// the buffer reaches back past it, from its oldest event otherwise.
func recoverFrom(
	buffered []events.Message,
	from, until time.Time,
	serviceStacks map[string]string,
) cache.Recovery {
	start := from.Add(eventReplayMargin)
	if len(buffered) == 0 || !until.After(start) {
		return cache.Recovery{}
	}

	recovery := cache.Recovery{From: start}
	if oldest := eventTime(buffered[0]); oldest.After(start) {
		recovery.From = oldest
	}

	if !recovery.From.Before(until) {
		return cache.Recovery{}
	}

	for _, msg := range buffered {
		typ, ok := recoverable[msg.Type]
		at := eventTime(msg)
		action := historyAction(msg.Action)

		if !ok || !at.After(start) || !at.Before(until) ||
			action != "create" && action != "update" && action != "remove" {
			continue
		}

		name := msg.Actor.Attributes["name"]
		if name == "" {
			name = msg.Actor.ID
		}

		entry := cache.HistoryEntry{
			Kind:       cache.KindChange,
			Timestamp:  at,
			Type:       typ,
			Action:     action,
			ResourceID: msg.Actor.ID,
			Name:       name,
			Recovered:  true,
		}
		if typ == cache.EventService {
			entry.Stack = serviceStacks[msg.Actor.ID]
		}

		recovery.Entries = append(recovery.Entries, entry)
	}

	return recovery
}

func eventTime(msg events.Message) time.Time {
	return time.Unix(0, msg.TimeNano)
}
