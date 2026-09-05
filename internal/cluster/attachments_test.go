package cluster

import (
	"testing"

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
