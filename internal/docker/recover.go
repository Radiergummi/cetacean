package docker

import (
	"context"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/events"

	"github.com/radiergummi/cetacean/internal/cache"
)

// recoverTimeout bounds the read of the daemon's buffer, which runs once, as
// the timeline records the gap it ends.
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

// EventBuffer reads what a daemon still holds of its recent events.
type EventBuffer interface {
	BufferedEvents(ctx context.Context, until time.Time) ([]events.Message, error)
}

// RecoverGap reads what the daemon still holds of the gap from from until
// now; a failed read recovers nothing, and the gap is recorded as it was.
func RecoverGap(ctx context.Context, buffer EventBuffer, from time.Time) cache.Recovery {
	ctx, cancel := context.WithTimeout(ctx, recoverTimeout)
	defer cancel()

	buffered, err := buffer.BufferedEvents(ctx, time.Now())
	if err != nil {
		slog.WarnContext(ctx, "could not read the changes made during the gap", "error", err)
		return cache.Recovery{}
	}

	return recoverFrom(buffered, from)
}

// recoverFrom turns the buffered events after from into recovered changes,
// complete from from when the buffer reaches back past it and from its oldest
// event otherwise: the buffer holds everything since that event, but not before.
func recoverFrom(buffered []events.Message, from time.Time) cache.Recovery {
	if len(buffered) == 0 {
		return cache.Recovery{}
	}

	recovery := cache.Recovery{From: from}
	if oldest := eventTime(buffered[0]); oldest.After(from) {
		recovery.From = oldest
	}

	for _, msg := range buffered {
		typ, ok := recoverable[msg.Type]
		at := eventTime(msg)

		if !ok || !at.After(from) {
			continue
		}

		action := string(msg.Action)
		if action == "destroy" {
			action = "remove"
		}

		if action != "create" && action != "update" && action != "remove" {
			continue
		}

		name := msg.Actor.Attributes["name"]
		if name == "" {
			name = msg.Actor.ID
		}

		recovery.Entries = append(recovery.Entries, cache.HistoryEntry{
			Kind:       cache.KindChange,
			Timestamp:  at,
			Type:       typ,
			Action:     action,
			ResourceID: msg.Actor.ID,
			Name:       name,
			Recovered:  true,
		})
	}

	return recovery
}

func eventTime(msg events.Message) time.Time {
	return time.Unix(0, msg.TimeNano)
}
