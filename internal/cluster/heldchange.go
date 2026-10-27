package cluster

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
)

// The *Change functions name, for CheckHostAccessHeld, what a write changes
// about what runs in a container, or return "" when it changes none of it.

// CommandChange compares what the container runs and as whom.
func CommandChange(before, after *swarm.ContainerSpec) string {
	if before == nil {
		before = &swarm.ContainerSpec{}
	}
	if after == nil {
		after = &swarm.ContainerSpec{}
	}

	if slices.Equal(before.Command, after.Command) && slices.Equal(before.Args, after.Args) &&
		before.Dir == after.Dir && before.User == after.User &&
		slices.Equal(before.Groups, after.Groups) {
		return ""
	}

	return "changing the command or user of"
}

// HealthcheckChange compares the command a healthcheck runs in the container.
func HealthcheckChange(before, after *container.HealthConfig) string {
	var was, is []string
	if before != nil {
		was = before.Test
	}
	if after != nil {
		is = after.Test
	}

	if slices.Equal(was, is) {
		return ""
	}

	return "changing the healthcheck command of"
}

// SecretsChange compares the secret files a container sees: a file placed over
// the entrypoint runs with the service's access as surely as a new image does.
func SecretsChange(before, after []*swarm.SecretReference) string {
	key := func(ref *swarm.SecretReference) string { return ref.SecretID + fileKey(ref.File) }
	if sameSet(before, after, key) {
		return ""
	}

	return "changing the secrets of"
}

// ConfigsChange is SecretsChange for configs.
func ConfigsChange(before, after []*swarm.ConfigReference) string {
	key := func(ref *swarm.ConfigReference) string {
		if ref.File == nil {
			return ref.ConfigID + "\x00runtime"
		}

		return ref.ConfigID + fileKey((*swarm.SecretReferenceFileTarget)(ref.File))
	}
	if sameSet(before, after, key) {
		return ""
	}

	return "changing the configs of"
}

// MountsChange compares the mounts a container sees, as CheckMounts reads them.
func MountsChange(before, after []mount.Mount) string {
	key := func(m mount.Mount) string {
		m = withoutEmptyBindOptions(m)
		m.Type = mount.Type(strings.ToLower(string(m.Type)))
		if m.Type == "" {
			m.Type = mount.TypeBind
		}

		encoded, _ := json.Marshal(m)

		return string(encoded)
	}
	if sameSet(before, after, key) {
		return ""
	}

	return "changing the mounts of"
}

func fileKey(f *swarm.SecretReferenceFileTarget) string {
	if f == nil {
		return "\x00"
	}

	encoded, _ := json.Marshal(f)

	return "\x00" + string(encoded)
}

// sameSet compares two lists as multisets of their keys; order is not a change.
func sameSet[T any](before, after []T, key func(T) string) bool {
	if len(before) != len(after) {
		return false
	}

	keys := func(list []T) []string {
		out := make([]string, len(list))
		for i, item := range list {
			out[i] = key(item)
		}
		slices.Sort(out)

		return out
	}

	return slices.Equal(keys(before), keys(after))
}
