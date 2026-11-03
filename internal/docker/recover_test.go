package docker

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/docker/docker/api/types/events"

	"github.com/radiergummi/cetacean/internal/cache"
)

var gapStart = time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)

func bufferedAt(
	minutes int,
	typ events.Type,
	action events.Action,
	id, name string,
) events.Message {
	return events.Message{
		Type:     typ,
		Action:   action,
		Actor:    events.Actor{ID: id, Attributes: map[string]string{"name": name}},
		TimeNano: gapStart.Add(time.Duration(minutes) * time.Minute).UnixNano(),
	}
}

func TestRecoverFrom_TurnsTheGapsEventsIntoRecoveredChanges(t *testing.T) {
	buffered := []events.Message{
		bufferedAt(-5, events.ServiceEventType, "update", "svc0", "before"),
		bufferedAt(10, events.ContainerEventType, "die", "c1", "web.1.x"),
		bufferedAt(20, events.ServiceEventType, "update", "svc1", "shop_web"),
		bufferedAt(30, events.NetworkEventType, "connect", "net1", "shop_default"),
		bufferedAt(40, events.VolumeEventType, "destroy", "data", ""),
	}

	got := recoverFrom(buffered, gapStart)

	if !got.From.Equal(gapStart) {
		t.Errorf("From = %v, want the gap's start, which the buffer reaches past", got.From)
	}

	want := []cache.HistoryEntry{
		{
			Kind: cache.KindChange, Timestamp: gapStart.Add(20 * time.Minute),
			Type: cache.EventService, Action: "update", ResourceID: "svc1", Name: "shop_web",
			Recovered: true,
		},
		{
			Kind: cache.KindChange, Timestamp: gapStart.Add(40 * time.Minute),
			Type: cache.EventVolume, Action: "remove", ResourceID: "data", Name: "data",
			Recovered: true,
		},
	}
	if len(got.Entries) != len(want) {
		t.Fatalf("entries = %+v, want %+v", got.Entries, want)
	}

	for i := range want {
		if !got.Entries[i].Timestamp.Equal(want[i].Timestamp) {
			t.Errorf("entry %d at %v, want %v", i, got.Entries[i].Timestamp, want[i].Timestamp)
		}

		got.Entries[i].Timestamp = want[i].Timestamp
		if !reflect.DeepEqual(got.Entries[i], want[i]) {
			t.Errorf("entry %d = %+v, want %+v", i, got.Entries[i], want[i])
		}
	}
}

func TestRecoverFrom_IsCompleteOnlyFromTheOldestEventItHolds(t *testing.T) {
	got := recoverFrom([]events.Message{
		bufferedAt(25, events.ContainerEventType, "start", "c1", "x"),
		bufferedAt(30, events.NodeEventType, "update", "node1", "worker-1"),
	}, gapStart)

	if want := gapStart.Add(25 * time.Minute); !got.From.Equal(want) {
		t.Errorf("From = %v, want the oldest buffered event, %v", got.From, want)
	}

	if len(got.Entries) != 1 || got.Entries[0].Name != "worker-1" {
		t.Errorf("entries = %+v, want the node's update", got.Entries)
	}

	if empty := recoverFrom(nil, gapStart); !empty.From.IsZero() || empty.Entries != nil {
		t.Errorf("an empty buffer recovered %+v, want nothing", empty)
	}
}

type failingBuffer struct{}

func (failingBuffer) BufferedEvents(context.Context, time.Time) ([]events.Message, error) {
	return nil, errors.New("daemon unreachable")
}

func TestRecoverGap_RecoversNothingWhenTheReadFails(t *testing.T) {
	if got := RecoverGap(context.Background(), failingBuffer{}, gapStart); !got.From.IsZero() ||
		got.Entries != nil {
		t.Errorf("a failed read recovered %+v, want nothing", got)
	}
}
