package cluster

import (
	"strings"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
)

// AttachmentKind is a resource type a service can attach, and its ACL prefix.
type AttachmentKind string

const (
	AttachSecret  AttachmentKind = "secret"
	AttachConfig  AttachmentKind = "config"
	AttachNetwork AttachmentKind = "network"
	AttachVolume  AttachmentKind = "volume"
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

// CheckVolumesAttachable applies CheckAttachable to every named volume a mount
// list references. Volumes are keyed by name, so the source is both.
func CheckVolumesAttachable(
	svc swarm.Service,
	canRead func(resource string) bool,
	mounts []mount.Mount,
) error {
	for _, m := range mounts {
		if !isVolumeMount(m) || m.Source == "" {
			continue
		}

		if err := CheckAttachable(svc, canRead, AttachVolume, m.Source, m.Source); err != nil {
			return err
		}
	}

	return nil
}

// isVolumeMount covers a Swarm cluster volume too: it is mounted by name and
// listed, and so keyed, as volume:<name> like any other.
func isVolumeMount(m mount.Mount) bool {
	switch mount.Type(strings.ToLower(string(m.Type))) {
	case mount.TypeVolume, mount.TypeCluster:
		return true
	default:
		return false
	}
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

	case AttachVolume:
		if spec.ContainerSpec != nil {
			for _, m := range spec.ContainerSpec.Mounts {
				if isVolumeMount(m) && m.Source == id {
					return true
				}
			}
		}
	}

	return false
}
