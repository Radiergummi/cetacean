package recommendations

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
)

func TestClusterChecker_SingleReplica(t *testing.T) {
	t.Run("service with 1 replica emits single-replica recommendation", func(t *testing.T) {
		c := cache.New(nil)
		replicas := uint64(1)
		c.SetService(swarm.Service{
			ID: "svc1",
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: "web"},
				Mode: swarm.ServiceMode{
					Replicated: &swarm.ReplicatedService{Replicas: &replicas},
				},
			},
		})

		checker := NewClusterChecker(c)
		recs := checker.Check(context.Background())

		found := false
		for _, rec := range recs {
			if rec.Category == CategorySingleReplica {
				found = true

				if rec.TargetID != "svc1" {
					t.Errorf("expected TargetID svc1, got %s", rec.TargetID)
				}

				if rec.Suggested == nil || *rec.Suggested != 2 {
					t.Errorf("expected Suggested=2, got %v", rec.Suggested)
				}

				if rec.FixAction == nil || *rec.FixAction != "PUT /services/{id}/scale" {
					t.Errorf("expected FixAction set, got %v", rec.FixAction)
				}
			}
		}

		if !found {
			t.Error("expected CategorySingleReplica recommendation, got none")
		}
	})

	t.Run("service with 3 replicas emits no single-replica recommendation", func(t *testing.T) {
		c := cache.New(nil)
		replicas := uint64(3)
		c.SetService(swarm.Service{
			ID: "svc1",
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: "web"},
				Mode: swarm.ServiceMode{
					Replicated: &swarm.ReplicatedService{Replicas: &replicas},
				},
			},
		})

		checker := NewClusterChecker(c)
		recs := checker.Check(context.Background())

		for _, rec := range recs {
			if rec.Category == CategorySingleReplica {
				t.Error("expected no CategorySingleReplica recommendation for 3-replica service")
			}
		}
	})

	t.Run("global service emits no single-replica recommendation", func(t *testing.T) {
		c := cache.New(nil)
		c.SetService(swarm.Service{
			ID: "svc1",
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: "web"},
				Mode:        swarm.ServiceMode{Global: &swarm.GlobalService{}},
			},
		})

		checker := NewClusterChecker(c)
		recs := checker.Check(context.Background())

		for _, rec := range recs {
			if rec.Category == CategorySingleReplica {
				t.Error("expected no CategorySingleReplica recommendation for global service")
			}
		}
	})
}

func TestClusterChecker_ManagerHasWorkloads(t *testing.T) {
	t.Run("manager node with active availability emits recommendation", func(t *testing.T) {
		c := cache.New(nil)
		c.SetNode(swarm.Node{
			ID: "node1",
			Spec: swarm.NodeSpec{
				Role:         swarm.NodeRoleManager,
				Availability: swarm.NodeAvailabilityActive,
			},
			Description: swarm.NodeDescription{Hostname: "manager-1"},
		})
		c.SetNode(swarm.Node{
			ID: "node2",
			Spec: swarm.NodeSpec{
				Role:         swarm.NodeRoleWorker,
				Availability: swarm.NodeAvailabilityActive,
			},
			Description: swarm.NodeDescription{Hostname: "worker-1"},
			Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
		})

		checker := NewClusterChecker(c)
		recs := checker.Check(context.Background())

		found := false
		for _, rec := range recs {
			if rec.Category == CategoryManagerHasWorkloads {
				found = true

				if rec.TargetID != "node1" {
					t.Errorf("expected TargetID node1, got %s", rec.TargetID)
				}

				if rec.FixAction == nil || *rec.FixAction != "PUT /nodes/{id}/availability" {
					t.Errorf("expected FixAction set, got %v", rec.FixAction)
				}
			}
		}

		if !found {
			t.Error("expected CategoryManagerHasWorkloads recommendation, got none")
		}
	})

	// Draining the only active node strands every task, so a single-node swarm
	// has nothing to move the manager's work to.
	t.Run("sole active node emits no recommendation", func(t *testing.T) {
		c := cache.New(nil)
		c.SetNode(swarm.Node{
			ID: "node1",
			Spec: swarm.NodeSpec{
				Role:         swarm.NodeRoleManager,
				Availability: swarm.NodeAvailabilityActive,
			},
			Description: swarm.NodeDescription{Hostname: "manager-1"},
		})

		for _, rec := range NewClusterChecker(c).Check(context.Background()) {
			if rec.Category == CategoryManagerHasWorkloads {
				t.Error("a single-node swarm was told to drain its only node")
			}
		}
	})

	// Draining every manager of a managers-only swarm strands the same tasks,
	// and so does draining towards a worker that is down.
	t.Run("no ready active worker emits no recommendation", func(t *testing.T) {
		c := cache.New(nil)
		for _, id := range []string{"m1", "m2", "m3"} {
			c.SetNode(swarm.Node{
				ID: id,
				Spec: swarm.NodeSpec{
					Role:         swarm.NodeRoleManager,
					Availability: swarm.NodeAvailabilityActive,
				},
				Status: swarm.NodeStatus{State: swarm.NodeStateReady},
			})
		}
		c.SetNode(swarm.Node{
			ID: "w1",
			Spec: swarm.NodeSpec{
				Role:         swarm.NodeRoleWorker,
				Availability: swarm.NodeAvailabilityActive,
			},
			Status: swarm.NodeStatus{State: swarm.NodeStateDown},
		})

		for _, rec := range NewClusterChecker(c).Check(context.Background()) {
			if rec.Category == CategoryManagerHasWorkloads {
				t.Errorf("manager %s told to drain with no worker to take over", rec.TargetID)
			}
		}
	})

	// A ready worker that is drained or paused accepts no new tasks either.
	for _, availability := range []swarm.NodeAvailability{swarm.NodeAvailabilityDrain, swarm.NodeAvailabilityPause} {
		t.Run("ready "+string(availability)+" worker emits no recommendation", func(t *testing.T) {
			c := cache.New(nil)
			c.SetNode(swarm.Node{
				ID: "m1",
				Spec: swarm.NodeSpec{
					Role:         swarm.NodeRoleManager,
					Availability: swarm.NodeAvailabilityActive,
				},
				Status: swarm.NodeStatus{State: swarm.NodeStateReady},
			})
			c.SetNode(swarm.Node{
				ID: "w1",
				Spec: swarm.NodeSpec{
					Role:         swarm.NodeRoleWorker,
					Availability: availability,
				},
				Status: swarm.NodeStatus{State: swarm.NodeStateReady},
			})

			for _, rec := range NewClusterChecker(c).Check(context.Background()) {
				if rec.Category == CategoryManagerHasWorkloads {
					t.Errorf(
						"manager %s told to drain towards a %s worker",
						rec.TargetID,
						availability,
					)
				}
			}
		})
	}

	t.Run("manager node with drain availability emits no recommendation", func(t *testing.T) {
		c := cache.New(nil)
		c.SetNode(swarm.Node{
			ID: "node1",
			Spec: swarm.NodeSpec{
				Role:         swarm.NodeRoleManager,
				Availability: swarm.NodeAvailabilityDrain,
			},
			Description: swarm.NodeDescription{Hostname: "manager-1"},
		})
		c.SetNode(swarm.Node{
			ID: "node2",
			Spec: swarm.NodeSpec{
				Role:         swarm.NodeRoleWorker,
				Availability: swarm.NodeAvailabilityActive,
			},
			Description: swarm.NodeDescription{Hostname: "worker-1"},
			Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
		})

		checker := NewClusterChecker(c)
		recs := checker.Check(context.Background())

		for _, rec := range recs {
			if rec.Category == CategoryManagerHasWorkloads {
				t.Error(
					"expected no CategoryManagerHasWorkloads recommendation for drained manager",
				)
			}
		}
	})

	t.Run("worker node with active availability emits no recommendation", func(t *testing.T) {
		c := cache.New(nil)
		c.SetNode(swarm.Node{
			ID: "node1",
			Spec: swarm.NodeSpec{
				Role:         swarm.NodeRoleWorker,
				Availability: swarm.NodeAvailabilityActive,
			},
			Description: swarm.NodeDescription{Hostname: "worker-1"},
		})

		checker := NewClusterChecker(c)
		recs := checker.Check(context.Background())

		for _, rec := range recs {
			if rec.Category == CategoryManagerHasWorkloads {
				t.Error("expected no CategoryManagerHasWorkloads recommendation for worker node")
			}
		}
	})
}

func TestClusterChecker_UnevenDistribution(t *testing.T) {
	t.Run("uneven distribution emits recommendation", func(t *testing.T) {
		c := cache.New(nil)

		// 9 tasks on node A
		for i := range 9 {
			c.SetTask(swarm.Task{
				ID:     "taskA" + string(rune('0'+i)),
				NodeID: "nodeA",
				Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
			})
		}

		// 2 tasks on node B
		c.SetTask(
			swarm.Task{
				ID:     "taskB0",
				NodeID: "nodeB",
				Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
			},
		)
		c.SetTask(
			swarm.Task{
				ID:     "taskB1",
				NodeID: "nodeB",
				Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
			},
		)

		checker := NewClusterChecker(c)
		recs := checker.Check(context.Background())

		found := false
		for _, rec := range recs {
			if rec.Category == CategoryUnevenDistribution {
				found = true

				if rec.Scope != ScopeCluster {
					t.Errorf("expected ScopeCluster, got %s", rec.Scope)
				}
			}
		}

		if !found {
			t.Error("expected CategoryUnevenDistribution recommendation, got none")
		}
	})

	t.Run("even distribution emits no recommendation", func(t *testing.T) {
		c := cache.New(nil)

		// 3 tasks on node A, 3 on node B
		for i := range 3 {
			c.SetTask(swarm.Task{
				ID:     "taskA" + string(rune('0'+i)),
				NodeID: "nodeA",
				Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
			})
			c.SetTask(swarm.Task{
				ID:     "taskB" + string(rune('0'+i)),
				NodeID: "nodeB",
				Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
			})
		}

		checker := NewClusterChecker(c)
		recs := checker.Check(context.Background())

		for _, rec := range recs {
			if rec.Category == CategoryUnevenDistribution {
				t.Error(
					"expected no CategoryUnevenDistribution recommendation for even distribution",
				)
			}
		}
	})
}
