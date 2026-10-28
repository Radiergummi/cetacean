package cluster

import (
	"slices"
	"strings"

	"github.com/docker/docker/api/types"

	"github.com/radiergummi/cetacean/internal/config"
)

// CheckPluginSettings refuses, below the impactful tier, a `plugin set` setting
// that lands on a mount source or a device path, both host paths. It resolves a
// name as the engine does: `name[.field][=value]`, matched against the plugin's
// env, then its mounts, then its devices, whatever field is given.
func CheckPluginSettings(
	plugin types.Plugin,
	settings []string,
	level config.OperationsLevel,
) error {
	if level >= config.OpsImpactful {
		return nil
	}

	for _, setting := range settings {
		name, _, _ := strings.Cut(setting, "=")
		if i := strings.LastIndex(name, "."); i > 0 {
			name = name[:i]
		}

		named := func(n string) bool { return n == name }
		switch {
		case slices.ContainsFunc(plugin.Config.Env, func(e types.PluginEnv) bool { return named(e.Name) }):
			continue
		case slices.ContainsFunc(plugin.Config.Mounts, func(m types.PluginMount) bool { return named(m.Name) }):
			return &HostAccessError{What: "setting the source of the plugin mount " + name}
		case slices.ContainsFunc(
			plugin.Config.Linux.Devices,
			func(d types.PluginDevice) bool { return named(d.Name) },
		):
			return &HostAccessError{What: "setting the path of the plugin device " + name}
		}
	}

	return nil
}
