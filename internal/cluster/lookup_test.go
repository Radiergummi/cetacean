package cluster_test

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
)

func TestLookupNamesANodeWithoutAHostnameByItsID(t *testing.T) {
	c := newTestCache()
	c.SetNode(swarm.Node{ID: "node1", Version: swarm.Version{Index: 4}})

	record, found, err := cluster.Lookup(c, "node", "node1")
	if err != nil || !found {
		t.Fatalf("Lookup(node, node1) = (_, %v, %v), want a record", found, err)
	}

	if record.Name != "node1" || record.Resource() != "node:node1" || record.Version != 4 {
		t.Errorf("Lookup(node, node1) = %+v, want it named and keyed by its ID", record)
	}
}

func TestLookupAnswersEveryKindByIDAndName(t *testing.T) {
	c := newTestCache()
	c.SetService(swarm.Service{
		ID:      "svc1",
		Version: swarm.Version{Index: 7},
		Spec:    swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	c.SetConfig(swarm.Config{
		ID:   "cfg1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "app-config"}},
	})

	for _, tc := range []struct{ kind, ident, id, resource string }{
		{"service", "svc1", "svc1", "service:web"},
		{"service", "web", "svc1", "service:web"},
		{"config", "app-config", "cfg1", "config:app-config"},
	} {
		record, found, err := cluster.Lookup(c, tc.kind, tc.ident)
		if err != nil || !found {
			t.Fatalf("Lookup(%s, %s) = (_, %v, %v)", tc.kind, tc.ident, found, err)
		}

		if record.ID != tc.id || record.Resource() != tc.resource ||
			record.WriteResource() != tc.resource {
			t.Errorf("Lookup(%s, %s) = %+v, want %s as %s", tc.kind, tc.ident, record,
				tc.id, tc.resource)
		}
	}
}

func TestTaskRecordIsWrittenAsItsServiceWhileOneExists(t *testing.T) {
	c := newTestCache()
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	live := cluster.TaskRecord(c, swarm.Task{ID: "t1", ServiceID: "svc1"})
	if live.Resource() != "task:t1" || live.WriteResource() != "service:web" {
		t.Errorf("live task = read %q, write %q; want task:t1, service:web",
			live.Resource(), live.WriteResource())
	}

	orphan := cluster.TaskRecord(c, swarm.Task{ID: "t2", ServiceID: "svc-gone"})
	if orphan.WriteResource() != "task:t2" {
		t.Errorf("orphaned task is written as %q, want task:t2", orphan.WriteResource())
	}
}

// A task's rendered name is its service's, so it must not resolve for a caller
// who cannot read that service — in a history query no more than anywhere else.
func TestResolveIdentifierResolvesATaskNameOnlyThroughAReadableService(t *testing.T) {
	c := newTestCache()
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	c.SetTask(swarm.Task{
		ID: "t1", ServiceID: "svc1", Slot: 1,
		DesiredState: swarm.TaskStateRunning,
		Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	blind := func(string) bool { return false }

	for _, resourceType := range []string{"task", ""} {
		id, err := cluster.ResolveIdentifier(c, resourceType, "web.1", blind)
		if err != nil {
			t.Fatalf("ResolveIdentifier(%q, web.1) = %v", resourceType, err)
		}

		if id != "web.1" {
			t.Errorf("ResolveIdentifier(%q, web.1) = %q for a caller who cannot read web",
				resourceType, id)
		}
	}

	if id, _ := cluster.ResolveIdentifier(c, "task", "web.1", nil); id != "t1" {
		t.Errorf("ResolveIdentifier(task, web.1) = %q, want t1", id)
	}

	if id, _ := cluster.ResolveIdentifier(c, "task", "t1", blind); id != "t1" {
		t.Errorf("ResolveIdentifier(task, t1) = %q, want the ID to stand", id)
	}
}

func TestLookupMissesAlikeForEveryKind(t *testing.T) {
	c := newTestCache()

	for _, kind := range []string{
		"service", "node", "task", "config", "secret", "network", "stack", "volume", "nonsense",
	} {
		record, found, err := cluster.Lookup(c, kind, "gone")
		if found || err != nil || record != (cluster.Record{}) {
			t.Errorf(
				"Lookup(%s, gone) = (%+v, %t, %v), want the zero record",
				kind,
				record,
				found,
				err,
			)
		}
	}
}
