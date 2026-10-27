package recommendations

import (
	"context"
	"fmt"
	"testing"
	"time"

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

func TestConfigChecker_UnusedVersion(t *testing.T) {
	day := func(n int) swarm.Meta {
		return swarm.Meta{CreatedAt: time.Date(2026, 1, n, 0, 0, 0, 0, time.UTC)}
	}

	c := cache.New(nil)
	for i, name := range []string{"db_v1", "db_v2", "db_v3", "lone_v1"} {
		c.SetSecret(swarm.Secret{
			ID:   fmt.Sprint("s", i+1),
			Meta: day(i + 1),
			Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: name}},
		})
	}
	for i, name := range []string{"app_0a1b2c3d", "app_4e5f6a7b"} {
		c.SetConfig(swarm.Config{
			ID:   fmt.Sprint("c", i+1),
			Meta: day(i + 1),
			Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: name}},
		})
	}
	c.SetService(swarm.Service{
		ID: "svc",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "api"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Secrets: []*swarm.SecretReference{{SecretID: "s2"}},
				},
			},
		},
	})

	got := map[string]Recommendation{}
	for _, r := range NewConfigChecker(c).Check(context.Background()) {
		if r.Category == CategoryUnusedVersion {
			got[r.TargetID] = r
		}
	}

	// Neither the newest nor a version a service still uses is flagged.
	if len(got) != 2 {
		t.Fatalf("expected findings for db_v1 and app_0a1b2c3d, got %+v", got)
	}

	sec := got["s1"]
	if sec.Scope != ScopeSecret || sec.Severity != SeverityInfo || sec.TargetName != "db_v1" {
		t.Errorf("unexpected secret finding %+v", sec)
	}

	cfg := got["c1"]
	if cfg.Scope != ScopeConfig || cfg.TargetName != "app_0a1b2c3d" {
		t.Errorf("unexpected config finding %+v", cfg)
	}
}
