package compose

import (
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
)

// testService is a fully populated service the whole suite reuses.
func testService() swarm.Service {
	grace := 20 * time.Second

	return swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "web_api",
				Labels: map[string]string{"com.docker.stack.namespace": "web", "tier": "front"},
			},
			Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: new(uint64(3))}},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Image:           "nginx:1.27@sha256:abc",
					Labels:          map[string]string{"com.docker.stack.namespace": "web"},
					Env:             []string{"PORT=80"},
					Hosts:           []string{"1.2.3.4 db"},
					StopGracePeriod: &grace,
					Healthcheck: &container.HealthConfig{
						Test:     []string{"CMD", "true"},
						Interval: 30 * time.Second,
					},
					Mounts: []mount.Mount{
						{Type: mount.TypeVolume, Source: "web_data", Target: "/data"},
					},
					Secrets: []*swarm.SecretReference{
						{
							SecretName: "web_token",
							File: &swarm.SecretReferenceFileTarget{
								Name: "token",
								Mode: 0o400,
							},
						},
					},
				},
				Resources: &swarm.ResourceRequirements{
					Limits: &swarm.Limit{NanoCPUs: 500000000, MemoryBytes: 536870912},
				},
				Networks: []swarm.NetworkAttachmentConfig{{Target: "web_internal"}},
			},
			EndpointSpec: &swarm.EndpointSpec{
				Mode: swarm.ResolutionModeVIP,
				Ports: []swarm.PortConfig{
					{
						TargetPort:    80,
						PublishedPort: 8080,
						Protocol:      "tcp",
						PublishMode:   swarm.PortConfigPublishModeIngress,
					},
				},
			},
		},
	}
}

// A single service creates nothing, so everything it names must be external —
// declaring an owned network here would make the deploy try to create one that
// already exists.
func TestFromServiceDeclaresEverythingExternal(t *testing.T) {
	f, _ := FromService(testService())

	if len(f.Services) != 1 {
		t.Fatalf("services = %d, want 1", len(f.Services))
	}
	if _, ok := f.Services["web_api"]; !ok {
		t.Errorf("service key = %v, want the full Swarm name", keys(f.Services))
	}
	if n := f.Networks["web_internal"]; !n.External {
		t.Errorf("network = %+v, want external", n)
	}
	if v := f.Volumes["web_data"]; !v.External {
		t.Errorf("volume = %+v, want external", v)
	}
	if s := f.Secrets["web_token"]; !s.External {
		t.Errorf("secret = %+v, want external", s)
	}
}

// The digest is what redeploys to the same running state.
func TestFromServiceKeepsTheImageDigest(t *testing.T) {
	f, _ := FromService(testService())

	if got := f.Services["web_api"].Image; got != "nginx:1.27@sha256:abc" {
		t.Errorf("image = %q, want the digest kept", got)
	}
}

// Swarm distinguishes container labels from service labels and hand-written
// files routinely conflate them; keeping them apart is part of the value.
func TestFromServiceSeparatesContainerAndServiceLabels(t *testing.T) {
	f, _ := FromService(testService())
	svc := f.Services["web_api"]

	if _, ok := svc.Labels["tier"]; ok {
		t.Error("a service label must not appear under labels")
	}
	if svc.Deploy == nil || svc.Deploy.Labels["tier"] != "front" {
		t.Errorf("deploy.labels = %v, want tier=front", svc.Deploy)
	}
}

func TestFromServiceCarriesDeploy(t *testing.T) {
	f, _ := FromService(testService())
	d := f.Services["web_api"].Deploy

	if d == nil || d.Replicas == nil || *d.Replicas != 3 {
		t.Fatalf("deploy = %+v, want 3 replicas", d)
	}
	if d.Mode != "replicated" {
		t.Errorf("mode = %q, want replicated", d.Mode)
	}
	if d.EndpointMode != "vip" {
		t.Errorf("endpoint_mode = %q, want vip", d.EndpointMode)
	}
	if d.Resources == nil || d.Resources.Limits.CPUs != "0.50" ||
		d.Resources.Limits.Memory != "512M" {
		t.Errorf("resources = %+v", d.Resources)
	}
}

// Runtime state a deploy would reject or ignore.
func TestFromServiceDropsRuntimeState(t *testing.T) {
	svc := testService()
	svc.Spec.TaskTemplate.ForceUpdate = 7

	f, _ := FromService(svc)
	out, err := Render(f, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	for _, banned := range []string{"ForceUpdate", "VirtualIPs", "UpdateStatus", "PreviousSpec", "svc1"} {
		if strings.Contains(string(out), banned) {
			t.Errorf("document carries runtime state %q:\n%s", banned, out)
		}
	}
}

// A plugin or network-attachment task has no ContainerSpec and is not a
// compose service; it must be omitted, not written out as an empty stanza.
func TestFromServiceOmitsServicesWithoutAContainerSpec(t *testing.T) {
	svc := swarm.Service{
		Spec: swarm.ServiceSpec{
			Annotations:  swarm.Annotations{Name: "plugin_svc"},
			TaskTemplate: swarm.TaskSpec{Runtime: "plugin"},
		},
	}

	f, warnings := FromService(svc)

	if _, ok := f.Services["plugin_svc"]; ok {
		t.Errorf("services = %v, want plugin_svc omitted", keys(f.Services))
	}
	if len(f.Services) != 0 {
		t.Errorf("services = %d, want none", len(f.Services))
	}

	found := false
	for _, w := range warnings {
		if strings.Contains(w, "plugin_svc") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want one naming plugin_svc", warnings)
	}

	out, err := Render(f, warnings)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(string(out), "plugin_svc: {}") {
		t.Errorf("document carries an empty service stanza:\n%s", out)
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return out
}
