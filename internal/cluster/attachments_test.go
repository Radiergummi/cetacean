package cluster

import (
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
)

// TestServiceDetailsReportsAttachmentNames: names, never content.
//
// A digest may say which secrets a container receives — that is what makes
// "who uses this secret?" and "did the rotation land?" answerable — without
// becoming a way to read them. The same rule envNames follows.
func TestServiceDetailsReportsAttachmentNames(t *testing.T) {
	svc := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Secrets: []*swarm.SecretReference{
						{SecretID: "s2", SecretName: "session_key"},
						{SecretID: "s1", SecretName: "db_password"},
					},
					Configs: []*swarm.ConfigReference{
						{ConfigID: "c1", ConfigName: "nginx_conf"},
					},
				},
			},
		},
	}

	got := ServiceDetails(svc)

	names, ok := got["secretNames"].([]string)
	if !ok {
		t.Fatalf(
			"secretNames = %v (%T), want a string slice",
			got["secretNames"],
			got["secretNames"],
		)
	}

	// Sorted, because a result may be cached by ETag and two reads of the
	// same service must serialise identically.
	if len(names) != 2 || names[0] != "db_password" || names[1] != "session_key" {
		t.Errorf("secretNames = %v, want [db_password session_key] sorted", names)
	}

	configs, ok := got["configNames"].([]string)
	if !ok || len(configs) != 1 || configs[0] != "nginx_conf" {
		t.Errorf("configNames = %v, want [nginx_conf]", got["configNames"])
	}
}

// A service with no attachments omits both keys rather than reporting empty
// slices, so "receives nothing" reads as absence — the convention every
// optional key in this projection follows.
func TestServiceDetailsOmitsAttachmentNamesWhenNone(t *testing.T) {
	svc := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations:  swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{}},
		},
	}

	got := ServiceDetails(svc)

	for _, key := range []string{"secretNames", "configNames"} {
		if _, present := got[key]; present {
			t.Errorf("%s present on a service with no attachments", key)
		}
	}
}

// A secret's value must never reach the projection, whatever Docker hands
// back on the reference.
func TestServiceDetailsNeverCarriesSecretContent(t *testing.T) {
	svc := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Secrets: []*swarm.SecretReference{{
						SecretID:   "s1",
						SecretName: "db_password",
						File: &swarm.SecretReferenceFileTarget{
							Name: "db_password",
							UID:  "0",
							GID:  "0",
							Mode: 0o444,
						},
					}},
				},
			},
		},
	}

	got := ServiceDetails(svc)

	names, _ := got["secretNames"].([]string)
	if len(names) != 1 || names[0] != "db_password" {
		t.Fatalf("secretNames = %v", got["secretNames"])
	}

	// The reference struct itself must not be handed through under any key:
	// it is the shape that would grow a payload field if Docker ever added
	// one.
	for key, value := range got {
		if _, isRef := value.([]*swarm.SecretReference); isRef {
			t.Errorf("key %q carries raw SecretReference structs", key)
		}
	}
}

// mountTargets is what confirms a mounts write landed.
//
// bindMounts already reports the security-relevant mounts in full, but it
// deliberately skips volumes — so on its own it cannot answer "is the data
// volume still mounted?" after a wholesale replacement, which is exactly the
// question a caller who just replaced the set is asking. A target path is a
// mount's identity here: two things cannot be mounted at one path.
func TestServiceDetailsReportsMountTargets(t *testing.T) {
	svc := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Mounts: []mount.Mount{
						{Type: mount.TypeVolume, Source: "data", Target: "/var/lib/data"},
						{Type: mount.TypeBind, Source: "/etc/certs", Target: "/certs"},
					},
				},
			},
		},
	}

	got := ServiceDetails(svc)

	targets, ok := got["mountTargets"].([]string)
	if !ok {
		t.Fatalf(
			"mountTargets = %v (%T), want a string slice",
			got["mountTargets"],
			got["mountTargets"],
		)
	}

	// Sorted, for the same reason the attachment names are: a result may be
	// cached by ETag and two reads must serialise identically.
	if len(targets) != 2 || targets[0] != "/certs" || targets[1] != "/var/lib/data" {
		t.Errorf("mountTargets = %v, want [/certs /var/lib/data] sorted", targets)
	}

	// The volume is the one bindMounts drops, which is why both keys answer
	// the section rather than either alone.
	binds, _ := got["bindMounts"].([]map[string]any)
	if len(binds) != 1 || binds[0]["target"] != "/certs" {
		t.Errorf("bindMounts = %v, want only the bind", got["bindMounts"])
	}
}

// A service that mounts nothing omits the key, like every other optional one.
func TestServiceDetailsOmitsMountTargetsWhenNone(t *testing.T) {
	svc := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations:  swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{}},
		},
	}

	if _, present := ServiceDetails(svc)["mountTargets"]; present {
		t.Error("mountTargets present on a service with no mounts")
	}
}
