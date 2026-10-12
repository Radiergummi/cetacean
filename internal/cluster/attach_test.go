package cluster

import (
	"errors"
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

func attachTestService() swarm.Service {
	return swarm.Service{
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Secrets: []*swarm.SecretReference{{SecretID: "s-own", SecretName: "own"}},
					Configs: []*swarm.ConfigReference{{ConfigID: "c-own", ConfigName: "own"}},
				},
				Networks: []swarm.NetworkAttachmentConfig{{Target: "n-own"}},
			},
		},
	}
}

func TestCheckAttachableRefusesUnreadable(t *testing.T) {
	readable := map[string]bool{"secret:mine": true, "config:mine": true, "network:mine": true}
	canRead := func(resource string) bool { return readable[resource] }

	for _, kind := range []AttachmentKind{AttachSecret, AttachConfig, AttachNetwork} {
		t.Run(string(kind), func(t *testing.T) {
			if err := CheckAttachable(
				attachTestService(),
				canRead,
				kind,
				"x-mine",
				"mine",
			); err != nil {
				t.Fatalf("readable %s refused: %v", kind, err)
			}

			err := CheckAttachable(attachTestService(), canRead, kind, "x-theirs", "theirs")

			var denied *AttachDeniedError
			if !errors.As(err, &denied) {
				t.Fatalf("unreadable %s: err = %v, want *AttachDeniedError", kind, err)
			}

			if want := string(kind) + ":theirs"; denied.Resource != want {
				t.Errorf("Resource = %q, want %q", denied.Resource, want)
			}
		})
	}
}

// A reference the service already carries is reachable through the service
// write grant regardless, so re-sending the list must not lock the caller out.
func TestCheckAttachableAdmitsWhatTheServiceAlreadyCarries(t *testing.T) {
	never := func(string) bool { return false }

	cases := map[AttachmentKind]string{
		AttachSecret:  "s-own",
		AttachConfig:  "c-own",
		AttachNetwork: "n-own",
	}
	for kind, id := range cases {
		if err := CheckAttachable(attachTestService(), never, kind, id, "own"); err != nil {
			t.Errorf("%s already attached as %s: %v", kind, id, err)
		}
	}
}
