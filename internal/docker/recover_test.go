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

var (
	gapStart  = time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	streamEnd = gapStart.Add(time.Hour)
)

func bufferedAt(
	at time.Duration,
	typ events.Type,
	action events.Action,
	id, name string,
) events.Message {
	return events.Message{
		Type:     typ,
		Action:   action,
		Actor:    events.Actor{ID: id, Attributes: map[string]string{"name": name}},
		TimeNano: gapStart.Add(at).UnixNano(),
	}
}

func TestRecoverFrom_TurnsTheGapsEventsIntoRecoveredChanges(t *testing.T) {
	buffered := []events.Message{
		bufferedAt(-5*time.Minute, events.ServiceEventType, "update", "svc0", "before"),
		bufferedAt(2*time.Second, events.ServiceEventType, "update", "svc0", "seen-before-the-gap"),
		bufferedAt(10*time.Minute, events.ContainerEventType, "die", "c1", "web.1.x"),
		bufferedAt(20*time.Minute, events.ServiceEventType, "remove", "svc1", "shop_web"),
		bufferedAt(30*time.Minute, events.NetworkEventType, "connect", "net1", "shop_default"),
		bufferedAt(40*time.Minute, events.VolumeEventType, "destroy", "data", ""),
		bufferedAt(time.Hour, events.ServiceEventType, "update", "svc2", "replayed-by-the-stream"),
	}

	got := recoverFrom(buffered, gapStart, streamEnd, map[string]string{"svc1": "shop"})

	if want := gapStart.Add(eventReplayMargin); !got.From.Equal(want) {
		t.Errorf("From = %v, want the gap's start past the replay margin, %v", got.From, want)
	}

	want := []cache.HistoryEntry{
		{
			Kind: cache.KindChange, Timestamp: gapStart.Add(20 * time.Minute),
			Type: cache.EventService, Action: "remove", ResourceID: "svc1", Name: "shop_web",
			Stack: "shop", Recovered: true,
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
		bufferedAt(25*time.Minute, events.ContainerEventType, "start", "c1", "x"),
		bufferedAt(30*time.Minute, events.NodeEventType, "update", "node1", "worker-1"),
	}, gapStart, streamEnd, nil)

	if want := gapStart.Add(25 * time.Minute); !got.From.Equal(want) {
		t.Errorf("From = %v, want the oldest buffered event, %v", got.From, want)
	}

	if len(got.Entries) != 1 || got.Entries[0].Name != "worker-1" {
		t.Errorf("entries = %+v, want the node's update", got.Entries)
	}
}

func TestRecoverFrom_RecoversNothingItCannotVouchFor(t *testing.T) {
	for name, got := range map[string]cache.Recovery{
		"an empty buffer": recoverFrom(nil, gapStart, streamEnd, nil),
		"a buffer starting after the stream": recoverFrom([]events.Message{
			bufferedAt(2*time.Hour, events.ServiceEventType, "update", "svc1", "web"),
		}, gapStart, streamEnd, nil),
		"a gap shorter than the margin": recoverFrom([]events.Message{
			bufferedAt(-time.Minute, events.ServiceEventType, "update", "svc1", "web"),
		}, gapStart, gapStart.Add(time.Second), nil),
	} {
		if !got.From.IsZero() || got.Entries != nil {
			t.Errorf("%s recovered %+v, want nothing", name, got)
		}
	}
}

type failingBuffer struct{}

func (failingBuffer) BufferedEvents(context.Context, time.Time) ([]events.Message, error) {
	return nil, errors.New("daemon unreachable")
}

func TestRecoverGap_RecoversNothingWhenTheReadFails(t *testing.T) {
	got := RecoverGap(context.Background(), failingBuffer{}, gapStart, streamEnd, nil)
	if !got.From.IsZero() || got.Entries != nil {
		t.Errorf("a failed read recovered %+v, want nothing", got)
	}
}
