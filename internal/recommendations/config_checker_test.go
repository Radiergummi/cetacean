package recommendations

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
)

func TestConfigChecker_NoHealthcheck(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations:  swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{},
		},
	})

	cc := NewConfigChecker(c)
	recs := cc.Check(context.Background())

	found := false
	for _, r := range recs {
		if r.Category == CategoryNoHealthcheck && r.TargetID == "svc1" {
			found = true

			if r.Scope != ScopeService {
				t.Errorf("expected scope %q, got %q", ScopeService, r.Scope)
			}

			if r.FixAction != nil {
				t.Errorf("expected nil FixAction, got %v", r.FixAction)
			}
		}
	}

	if !found {
		t.Errorf("expected no-healthcheck recommendation, got: %+v", recs)
	}
}

func TestConfigChecker_NoneHealthcheck(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc2",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "worker"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Healthcheck: &container.HealthConfig{
						Test: []string{"NONE"},
					},
				},
			},
		},
	})

	cc := NewConfigChecker(c)
	recs := cc.Check(context.Background())

	found := false
	for _, r := range recs {
		if r.Category == CategoryNoHealthcheck && r.TargetID == "svc2" {
			found = true
		}
	}

	if !found {
		t.Errorf("expected no-healthcheck recommendation for NONE healthcheck, got: %+v", recs)
	}
}

func TestConfigChecker_NoRestartPolicy(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc3",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "job"},
			TaskTemplate: swarm.TaskSpec{
				RestartPolicy: &swarm.RestartPolicy{
					Condition: swarm.RestartPolicyConditionNone,
				},
				ContainerSpec: &swarm.ContainerSpec{
					Healthcheck: &container.HealthConfig{
						Test: []string{"CMD", "echo", "ok"},
					},
				},
			},
		},
	})

	cc := NewConfigChecker(c)
	recs := cc.Check(context.Background())

	found := false
	for _, r := range recs {
		if r.Category == CategoryNoRestartPolicy && r.TargetID == "svc3" {
			found = true

			if r.Scope != ScopeService {
				t.Errorf("expected scope %q, got %q", ScopeService, r.Scope)
			}

			if r.FixAction != nil {
				t.Errorf("expected nil FixAction, got %v", r.FixAction)
			}
		}
	}

	if !found {
		t.Errorf("expected no-restart-policy recommendation, got: %+v", recs)
	}
}

func TestConfigChecker_NilRestartPolicyIsDefault(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc-nil-rp",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "default-restart"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Healthcheck: &container.HealthConfig{
						Test: []string{"CMD", "true"},
					},
				},
				// RestartPolicy is nil — Docker defaults to "any", should NOT be flagged
			},
		},
	})

	cc := NewConfigChecker(c)
	recs := cc.Check(context.Background())

	for _, r := range recs {
		if r.Category == CategoryNoRestartPolicy && r.TargetID == "svc-nil-rp" {
			t.Error(
				"nil RestartPolicy should not trigger no-restart-policy (Docker defaults to 'any')",
			)
		}
	}
}

func TestConfigChecker_HealthyService(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc4",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "api"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Healthcheck: &container.HealthConfig{
						Test: []string{"CMD", "wget", "-q", "-O", "-", "http://localhost/health"},
					},
				},
				RestartPolicy: &swarm.RestartPolicy{
					Condition: swarm.RestartPolicyConditionAny,
				},
			},
		},
	})

	cc := NewConfigChecker(c)
	recs := cc.Check(context.Background())

	for _, r := range recs {
		if r.TargetID == "svc4" {
			t.Errorf("expected no recommendations for healthy service, got: %+v", r)
		}
	}
}

func TestConfigChecker_AllRecsHaveScopeService(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc5",
		Spec: swarm.ServiceSpec{
			Annotations:  swarm.Annotations{Name: "bare"},
			TaskTemplate: swarm.TaskSpec{},
		},
	})

	cc := NewConfigChecker(c)
	recs := cc.Check(context.Background())

	if len(recs) == 0 {
		t.Fatal("expected at least one recommendation")
	}

	for _, r := range recs {
		if r.Scope != ScopeService {
			t.Errorf("expected scope %q, got %q", ScopeService, r.Scope)
		}

		if r.FixAction != nil {
			t.Errorf("expected nil FixAction, got %v", r.FixAction)
		}
	}
}
