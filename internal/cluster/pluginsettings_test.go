package cluster

import (
	"errors"
	"testing"

	"github.com/docker/docker/api/types"

	"github.com/radiergummi/cetacean/internal/config"
)

func settablePlugin() types.Plugin {
	return types.Plugin{Config: types.PluginConfig{
		Env: []types.PluginEnv{{Name: "DEBUG", Settable: []string{"value"}}},
		Args: types.PluginConfigArgs{
			Name:     "args",
			Settable: []string{"value"},
		},
		Mounts: []types.PluginMount{{Name: "state", Settable: []string{"source"}}},
		Linux: types.PluginConfigLinux{
			Devices: []types.PluginDevice{{Name: "disk", Settable: []string{"path"}}},
		},
	}}
}

func TestCheckPluginSettings(t *testing.T) {
	cases := []struct {
		name     string
		settings []string
		level    config.OperationsLevel
		refused  bool
	}{
		{"env at tier 2", []string{"DEBUG=1"}, config.OpsConfiguration, false},
		{"env with its field at tier 2", []string{"DEBUG.value=1"}, config.OpsConfiguration, false},
		{"args at tier 2", []string{"args=--verbose --x"}, config.OpsConfiguration, false},
		{"mount source at tier 2", []string{"state.source=/"}, config.OpsConfiguration, true},
		// Docker defaults the field when the mount has only one settable.
		{"mount by bare name at tier 2", []string{"state=/"}, config.OpsConfiguration, true},
		{"device path at tier 2", []string{"disk.path=/dev/sda"}, config.OpsConfiguration, true},
		{"device by bare name at tier 2", []string{"disk=/dev/sda"}, config.OpsConfiguration, true},
		{
			"mount after env at tier 2",
			[]string{"DEBUG=1", "state=/"},
			config.OpsConfiguration,
			true,
		},
		{"mount source at tier 3", []string{"state.source=/"}, config.OpsImpactful, false},
		{"unknown name at tier 2", []string{"nothing=1"}, config.OpsConfiguration, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckPluginSettings(settablePlugin(), tc.settings, tc.level)

			var denied *HostAccessError
			if refused := errors.As(err, &denied); refused != tc.refused {
				t.Errorf("err = %v, want refused=%v", err, tc.refused)
			}
		})
	}
}

// Docker resolves a name against env first, so an env sharing a mount's name
// is set as the env, and a mount field on it is refused by Docker itself.
func TestCheckPluginSettingsResolvesEnvFirst(t *testing.T) {
	plugin := settablePlugin()
	plugin.Config.Env = append(plugin.Config.Env, types.PluginEnv{Name: "state"})

	if err := CheckPluginSettings(
		plugin,
		[]string{"state=x"},
		config.OpsConfiguration,
	); err != nil {
		t.Errorf("env shadowing a mount refused: %v", err)
	}
}
