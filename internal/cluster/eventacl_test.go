package cluster

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

func TestCanReadEvent(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp"}},
	})

	var live, removed cache.Event
	c.AddOnChangeListener(func(ev cache.Event) {
		if ev.Type != cache.EventTask {
			return
		}
		if ev.Action == "remove" {
			removed = ev
		} else {
			live = ev
		}
	})
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1"})
	c.DeleteTask("t1")

	onlyWebapp := func(resource string) bool { return resource == "service:webapp" }

	if !CanReadEvent(c, removed, onlyWebapp) {
		t.Error("a removed task should be readable through the service it carries")
	}

	if CanReadEvent(c, live, onlyWebapp) {
		t.Error("a live task is resolved by the evaluator; the fallback is for removes only")
	}

	if !CanReadEvent(c, removed, func(r string) bool { return r == "task:t1" }) {
		t.Error("the event's own resource should be checked first")
	}

	if CanReadEvent(c, removed, func(string) bool { return false }) {
		t.Error("nothing readable should admit nothing")
	}
}
