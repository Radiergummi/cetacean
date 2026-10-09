package cluster

import "github.com/docker/docker/api/types/swarm"

// AttachmentKind is a resource type a service can attach, and its ACL prefix.
type AttachmentKind string

const (
	AttachSecret  AttachmentKind = "secret"
	AttachConfig  AttachmentKind = "config"
	AttachNetwork AttachmentKind = "network"
)

// AttachDeniedError reports an attachment the caller may not read.
type AttachDeniedError struct {
	Resource string
}

func (e *AttachDeniedError) Error() string {
	return "read access denied for " + e.Resource
}

// CheckAttachable refuses an attachment the caller may not read, so a service
// write grant cannot mount a credential or join a network it cannot see. What
// the service already carries is exempt: its write grant reaches that anyway.
func CheckAttachable(
	svc swarm.Service,
	canRead func(resource string) bool,
	kind AttachmentKind,
	id, name string,
) error {
	if attached(svc, kind, id) {
		return nil
	}

	resource := string(kind) + ":" + name
	if !canRead(resource) {
		return &AttachDeniedError{Resource: resource}
	}

	return nil
}

func attached(svc swarm.Service, kind AttachmentKind, id string) bool {
	spec := svc.Spec.TaskTemplate

	switch kind {
	case AttachSecret:
		if spec.ContainerSpec != nil {
			for _, ref := range spec.ContainerSpec.Secrets {
				if ref != nil && ref.SecretID == id {
					return true
				}
			}
		}

	case AttachConfig:
		if spec.ContainerSpec != nil {
			for _, ref := range spec.ContainerSpec.Configs {
				if ref != nil && ref.ConfigID == id {
					return true
				}
			}
		}

	case AttachNetwork:
		for _, ref := range spec.Networks {
			if ref.Target == id {
				return true
			}
		}
	}

	return false
}
