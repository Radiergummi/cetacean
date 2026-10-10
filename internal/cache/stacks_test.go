package cache

import (
	"slices"
	"sync"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

func stackLabels(ns string) map[string]string {
	return map[string]string{stackLabel: ns}
}

func stackService(id, ns string) swarm.Service {
	return swarm.Service{ID: id, Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{
		Name: id, Labels: stackLabels(ns),
	}}}
}

// assertStackMembers checks every member kind the stack fixtures populate.
func assertStackMembers(t *testing.T, c *Cache, ns string) {
	t.Helper()

	s, ok := c.GetStack(ns)
	if !ok {
		t.Fatalf("stack %q missing", ns)
	}

	for kind, got := range map[string][]string{
		"configs":  s.Configs,
		"secrets":  s.Secrets,
		"networks": s.Networks,
		"volumes":  s.Volumes,
	} {
		if !slices.Equal(got, []string{kind + "-1"}) {
			t.Errorf("stack %s = %v, want [%s-1]", kind, got, kind)
		}
	}
}

func setStackMembers(c *Cache, ns string, wg *sync.WaitGroup) {
	labels := stackLabels(ns)
	sets := []func(){
		func() {
			c.SetConfig(swarm.Config{ID: "configs-1", Spec: swarm.ConfigSpec{
				Annotations: swarm.Annotations{Name: "c", Labels: labels},
			}})
		},
		func() {
			c.SetSecret(swarm.Secret{ID: "secrets-1", Spec: swarm.SecretSpec{
				Annotations: swarm.Annotations{Name: "s", Labels: labels},
			}})
		},
		func() { c.SetNetwork(network.Summary{ID: "networks-1", Name: "n", Labels: labels}) },
		func() { c.SetVolume(volume.Volume{Name: "volumes-1", Labels: labels}) },
	}

	for _, set := range sets {
		if wg == nil {
			set()
			continue
		}

		wg.Go(set)
	}
}

// `docker stack deploy` creates configs, secrets and networks before the
// service that makes the stack exist; they must not be dropped for arriving first.
func TestStackKeepsMembersCreatedBeforeItsService(t *testing.T) {
	c := New(nil)

	setStackMembers(c, "app", nil)
	c.SetService(stackService("svc-1", "app"))

	assertStackMembers(t, c, "app")
}

// Updating a stack's only service removes and re-adds it; the members stay.
func TestStackKeepsMembersAcrossItsOnlyServiceUpdating(t *testing.T) {
	c := New(nil)

	c.SetService(stackService("svc-1", "app"))
	setStackMembers(c, "app", nil)
	c.SetService(stackService("svc-1", "app"))

	assertStackMembers(t, c, "app")
}

// The watcher applies a batch across several workers, so a stack's members and
// its service land in any order.
func TestStackMembershipIsIndependentOfConcurrentArrivalOrder(t *testing.T) {
	for range 200 {
		c := New(nil)

		var wg sync.WaitGroup
		setStackMembers(c, "app", &wg)
		wg.Go(func() { c.SetService(stackService("svc-1", "app")) })
		wg.Wait()

		assertStackMembers(t, c, "app")

		if t.Failed() {
			return
		}
	}
}

// A finished job is done, not degraded: it has nothing it keeps running.
func TestStackSummaryDesiresNothingOfAJob(t *testing.T) {
	c := New(nil)

	job := stackService("job-1", "app")
	job.Spec.Mode = swarm.ServiceMode{ReplicatedJob: &swarm.ReplicatedJob{}}
	c.SetService(job)

	summaries := c.ListStackSummaries()
	if len(summaries) != 1 {
		t.Fatalf("summaries = %d, want 1", len(summaries))
	}

	if got := summaries[0].DesiredTasks; got != 0 {
		t.Errorf("DesiredTasks = %d, want 0 for a job", got)
	}
}

// Only the current run's completions count: a re-run bumps JobIteration and
// leaves the previous run's completed tasks in the task list.
func TestCompletedJobTaskCountFollowsTheCurrentIteration(t *testing.T) {
	c := New(nil)

	job := stackService("job-1", "app")
	job.Spec.Mode = swarm.ServiceMode{ReplicatedJob: &swarm.ReplicatedJob{}}
	job.JobStatus = &swarm.JobStatus{JobIteration: swarm.Version{Index: 7}}
	c.SetService(job)

	task := func(id string, iteration uint64, state swarm.TaskState) swarm.Task {
		return swarm.Task{
			ID:           id,
			ServiceID:    "job-1",
			JobIteration: &swarm.Version{Index: iteration},
			DesiredState: swarm.TaskStateComplete,
			Status:       swarm.TaskStatus{State: state},
		}
	}

	c.SetTask(task("old", 3, swarm.TaskStateComplete))
	c.SetTask(task("done", 7, swarm.TaskStateComplete))
	c.SetTask(task("busy", 7, swarm.TaskStateRunning))

	if got := c.CompletedJobTaskCount("job-1"); got != 1 {
		t.Errorf("CompletedJobTaskCount = %d, want 1", got)
	}

	if got := c.CompletedJobTaskCounts()["job-1"]; got != 1 {
		t.Errorf("CompletedJobTaskCounts = %d, want 1", got)
	}

	if got := c.Snapshot().CompletedJobsByService["job-1"]; got != 1 {
		t.Errorf("Snapshot().CompletedJobsByService = %d, want 1", got)
	}
}
