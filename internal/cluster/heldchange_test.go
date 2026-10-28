package cluster

import (
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
)

func TestCommandChange(t *testing.T) {
	base := swarm.ContainerSpec{
		Command: []string{"nginx"},
		Args:    []string{"-g", "daemon off;"},
		Dir:     "/srv",
		User:    "www",
		Groups:  []string{"web"},
	}
	cases := []struct {
		name    string
		mutate  func(*swarm.ContainerSpec)
		changed bool
	}{
		{"nothing", func(*swarm.ContainerSpec) {}, false},
		{"hostname", func(cs *swarm.ContainerSpec) { cs.Hostname = "web" }, false},
		{"command", func(cs *swarm.ContainerSpec) { cs.Command = []string{"sh"} }, true},
		{"args", func(cs *swarm.ContainerSpec) { cs.Args = nil }, true},
		{"dir", func(cs *swarm.ContainerSpec) { cs.Dir = "/tmp" }, true},
		{"user", func(cs *swarm.ContainerSpec) { cs.User = "root" }, true},
		{"groups", func(cs *swarm.ContainerSpec) { cs.Groups = []string{"docker"} }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			after := base
			tc.mutate(&after)

			if changed := CommandChange(&base, &after) != ""; changed != tc.changed {
				t.Errorf("changed = %v, want %v", changed, tc.changed)
			}
		})
	}

	if CommandChange(nil, &swarm.ContainerSpec{}) != "" {
		t.Error("an empty spec differs from none")
	}
}

func TestHealthcheckChange(t *testing.T) {
	probe := &container.HealthConfig{Test: []string{"CMD", "true"}, Retries: 3}

	if HealthcheckChange(probe, &container.HealthConfig{Test: probe.Test, Retries: 5}) != "" {
		t.Error("a retry count change counted as a command change")
	}
	if HealthcheckChange(probe, &container.HealthConfig{Test: []string{"CMD", "sh"}}) == "" {
		t.Error("a new test command went unnoticed")
	}
	if HealthcheckChange(nil, &container.HealthConfig{Interval: 1}) != "" {
		t.Error("an interval alone counted as a command change")
	}
}

func TestEnvLabelsPortsAndNetworksChanges(t *testing.T) {
	if EnvChange(map[string]string{"A": "1"}, map[string]string{"A": "1"}) != "" {
		t.Error("an unchanged environment counted as a change")
	}
	if EnvChange(map[string]string{"A": "1"}, map[string]string{"A": "2"}) == "" {
		t.Error("a changed variable went unnoticed")
	}
	if LabelsChange(nil, map[string]string{}) != "" {
		t.Error("an empty label set counted as a change")
	}
	if LabelsChange(nil, map[string]string{"traefik.enable": "true"}) == "" {
		t.Error("an added label went unnoticed")
	}

	web := swarm.PortConfig{Protocol: "tcp", TargetPort: 80, PublishedPort: 8080}
	api := swarm.PortConfig{Protocol: "tcp", TargetPort: 2375, PublishedPort: 2375}
	if PortsChange([]swarm.PortConfig{web, api}, []swarm.PortConfig{api, web}) != "" {
		t.Error("reordering ports counted as a change")
	}
	if PortsChange([]swarm.PortConfig{web}, []swarm.PortConfig{web, api}) == "" {
		t.Error("a published port went unnoticed")
	}

	front := swarm.NetworkAttachmentConfig{Target: "n1", Aliases: []string{"a", "b"}}
	reordered := swarm.NetworkAttachmentConfig{Target: "n1", Aliases: []string{"b", "a"}}
	if NetworksChange(
		[]swarm.NetworkAttachmentConfig{front},
		[]swarm.NetworkAttachmentConfig{reordered},
	) != "" {
		t.Error("reordering aliases counted as a change")
	}
	if NetworksChange(nil, []swarm.NetworkAttachmentConfig{front}) == "" {
		t.Error("an attached network went unnoticed")
	}
}

func TestAttachmentChanges(t *testing.T) {
	secret := func(id, name string) *swarm.SecretReference {
		return &swarm.SecretReference{
			SecretID: id,
			File:     &swarm.SecretReferenceFileTarget{Name: name, UID: "0", GID: "0", Mode: 0o444},
		}
	}
	config := func(id, name string) *swarm.ConfigReference {
		return &swarm.ConfigReference{
			ConfigID: id,
			File:     &swarm.ConfigReferenceFileTarget{Name: name, UID: "0", GID: "0", Mode: 0o444},
		}
	}
	a, b := secret("a", "/run/secrets/a"), secret("b", "/run/secrets/b")

	if SecretsChange([]*swarm.SecretReference{a, b}, []*swarm.SecretReference{b, a}) != "" {
		t.Error("reordering secrets counted as a change")
	}
	if SecretsChange([]*swarm.SecretReference{a}, []*swarm.SecretReference{a, b}) == "" {
		t.Error("an added secret went unnoticed")
	}
	if SecretsChange(
		[]*swarm.SecretReference{a},
		[]*swarm.SecretReference{secret("a", "/docker-entrypoint.sh")},
	) == "" {
		t.Error("a moved secret went unnoticed")
	}

	entry := config("c", "/docker-entrypoint.sh")
	if ConfigsChange(nil, []*swarm.ConfigReference{entry}) == "" {
		t.Error("a config over the entrypoint went unnoticed")
	}
	if ConfigsChange([]*swarm.ConfigReference{entry}, []*swarm.ConfigReference{entry}) != "" {
		t.Error("an unchanged config counted as a change")
	}

	bind := mount.Mount{Type: mount.TypeBind, Source: "/srv", Target: "/srv"}
	optioned := bind
	optioned.BindOptions = &mount.BindOptions{}
	upper := bind
	upper.Type = "BIND"
	tmpfs := mount.Mount{Type: mount.TypeTmpfs, Target: "/docker-entrypoint.sh"}

	if MountsChange([]mount.Mount{bind}, []mount.Mount{optioned}) != "" {
		t.Error("an empty bind options block counted as a change")
	}
	if MountsChange([]mount.Mount{bind}, []mount.Mount{upper}) != "" {
		t.Error("the type's spelling counted as a change")
	}
	if MountsChange([]mount.Mount{bind}, []mount.Mount{bind, tmpfs}) == "" {
		t.Error("an added mount went unnoticed")
	}
}
