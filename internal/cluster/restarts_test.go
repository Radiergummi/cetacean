package cluster

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

// TestServiceDigestReportsRestartCounts pins what the live evaluation found
// missing: a service whose command exits every few seconds described as
// `"state": "running"` with a single recent failure, because
// DeriveServiceState reports "running" whenever a replica happens to be up and
// ServiceDigest deliberately drops tasks the orchestrator has already replaced
// (they explain history, not the current state).
//
// Both of those are right on their own terms, and together they hide a
// restart loop completely. The counts are the fact that does not: they are
// what "is this flapping?" actually asks.
func TestServiceDigestReportsRestartCounts(t *testing.T) {
	svc := swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "demo_flaky"}},
	}
	running := []swarm.Task{{
		ID:        "t1",
		ServiceID: "svc1",
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
	}}

	got := ServiceDigest(svc, running, nil, &ServiceRestarts{LastHour: 12, LastWeek: 840})

	if got.Restarts == nil {
		t.Fatal("Restarts is nil; a caller cannot tell a flapping service from a healthy one")
	}
	if got.Restarts.LastHour != 12 {
		t.Errorf("LastHour = %d, want 12", got.Restarts.LastHour)
	}
	if got.Restarts.LastWeek != 840 {
		t.Errorf("LastWeek = %d, want 840", got.Restarts.LastWeek)
	}
}

// TestServiceDigestOmitsRestartsWhenUnknown keeps the field out of the payload
// for a caller that has no tracker to read, rather than reporting a confident
// zero — "never restarted" and "not measured" are different answers.
func TestServiceDigestOmitsRestartsWhenUnknown(t *testing.T) {
	svc := swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	}

	got := ServiceDigest(svc, nil, nil, nil)

	if got.Restarts != nil {
		t.Errorf("Restarts = %+v, want nil when no tracker was consulted", got.Restarts)
	}
}

// TestServiceRestartsSeparatesRecentFromChronic is the intent the two windows
// exist for: "new, or has it always been like this?" One number cannot answer
// it, so the digest carries a short window and a long one.
func TestServiceRestartsSeparatesRecentFromChronic(t *testing.T) {
	svc := swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	}

	newlyBroken := ServiceDigest(svc, nil, nil, &ServiceRestarts{LastHour: 20, LastWeek: 20})
	longBroken := ServiceDigest(svc, nil, nil, &ServiceRestarts{LastHour: 20, LastWeek: 900})

	if newlyBroken.Restarts.LastWeek != newlyBroken.Restarts.LastHour {
		t.Error("a fault that started within the hour must not look chronic")
	}
	if longBroken.Restarts.LastWeek <= longBroken.Restarts.LastHour {
		t.Error("a long-standing fault must be distinguishable from a new one")
	}
}
