package cluster

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

func TestCanReadEvent(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{
			Name:   "shop_web",
			Labels: map[string]string{"com.docker.stack.namespace": "shop"},
		}},
	})

	var live, removed, removedService cache.Event
	c.AddOnChangeListener(func(ev cache.Event) {
		switch {
		case ev.Type == cache.EventService && ev.Action == "remove":
			removedService = ev
		case ev.Type != cache.EventTask:
		case ev.Action == "remove":
			removed = ev
		default:
			live = ev
		}
	})
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1"})
	c.DeleteTask("t1")
	c.DeleteService("svc1")

	onlyService := func(resource string) bool { return resource == "service:shop_web" }
	onlyStack := func(resource string) bool { return resource == "stack:shop" }

	if !CanReadEvent(removed, onlyService) {
		t.Error("a removed task should be readable through its service")
	}

	if !CanReadEvent(removed, onlyStack) {
		t.Error("a removed task should be readable through its service's stack")
	}

	if !CanReadEvent(removedService, onlyStack) {
		t.Error("a removed service should be readable through its stack")
	}

	if CanReadEvent(live, onlyService) {
		t.Error("a live task is resolved by the evaluator; the fallback is for removes only")
	}

	if !CanReadEvent(removed, func(r string) bool { return r == "task:t1" }) {
		t.Error("the event's own resource should be checked first")
	}

	if CanReadEvent(removed, func(string) bool { return false }) {
		t.Error("nothing readable should admit nothing")
	}
}
