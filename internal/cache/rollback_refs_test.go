package cache

import (
	"slices"
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

func usingSecrets(current, previous string) swarm.Service {
	spec := func(id string) swarm.ServiceSpec {
		return swarm.ServiceSpec{
			Name: "api",
			TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{
				Secrets: []*swarm.SecretReference{{SecretID: id}},
			}},
		}
	}

	svc := swarm.Service{ID: "svc", Spec: spec(current)}
	if previous != "" {
		svc.PreviousSpec = new(spec(previous))
	}

	return svc
}

// Rotating twice moves v1 from the current spec to the previous one and then
// out: each step is a change to who uses it, and the rollback flag follows.
func TestServicesUsingSecretFollowsAPreviousSpec(t *testing.T) {
	var changed []string
	c := New(func(e Event) {
		if e.Action == "ref_changed" {
			changed = append(changed, e.ID)
		}
	})

	c.SetService(usingSecrets("v1", ""))
	changed = nil

	c.SetService(usingSecrets("v2", "v1"))
	if users := c.ServicesUsingSecret("v1"); len(users) != 1 || !users[0].Rollback {
		t.Errorf(
			"after v1→v2, ServicesUsingSecret(v1) = %+v, want the service, for a rollback",
			users,
		)
	}
	if users := c.ServicesUsingSecret("v2"); len(users) != 1 || users[0].Rollback {
		t.Errorf("after v1→v2, ServicesUsingSecret(v2) = %+v, want the service", users)
	}
	slices.Sort(changed)
	if !slices.Equal(changed, []string{"v1", "v2"}) {
		t.Errorf("after v1→v2, ref_changed for %v, want v1 and v2", changed)
	}

	changed = nil
	c.SetService(usingSecrets("v3", "v2"))
	if users := c.ServicesUsingSecret("v1"); len(users) != 0 {
		t.Errorf("after v2→v3, ServicesUsingSecret(v1) = %+v, want none", users)
	}
	if !slices.Contains(changed, "v1") {
		t.Errorf("after v2→v3, ref_changed for %v, want v1 among them", changed)
	}
}

func TestRemovingAServiceDropsItsPreviousSpecReferences(t *testing.T) {
	var changed []string
	c := New(func(e Event) {
		if e.Action == "ref_changed" {
			changed = append(changed, e.ID)
		}
	})

	c.SetService(usingSecrets("v2", "v1"))
	changed = nil

	c.DeleteService("svc")

	if users := c.ServicesUsingSecret("v1"); len(users) != 0 {
		t.Errorf("ServicesUsingSecret(v1) = %+v after the service went, want none", users)
	}
	if !slices.Contains(changed, "v1") {
		t.Errorf("ref_changed for %v, want v1 among them", changed)
	}
}

// Config and secret versions are derived from configs, secrets and services
// alone, so a task changing leaves their counter where it was.
func TestVersionGenerationIgnoresTasks(t *testing.T) {
	c := New(nil)
	before := c.VersionGeneration()

	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc"})
	if c.VersionGeneration() != before {
		t.Error("a task event advanced the version generation")
	}

	c.SetSecret(swarm.Secret{ID: "s1"})
	if c.VersionGeneration() == before {
		t.Error("a secret event left the version generation where it was")
	}
}
