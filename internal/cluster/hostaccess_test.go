package cluster

import (
	"errors"
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/config"
)

func hostAccessService() swarm.Service {
	return swarm.Service{Spec: swarm.ServiceSpec{TaskTemplate: swarm.TaskSpec{
		ContainerSpec: &swarm.ContainerSpec{
			Mounts: []mount.Mount{
				{Type: mount.TypeBind, Source: "/srv/data", Target: "/data"},
				{
					Type:        mount.TypeBind,
					Source:      "/srv/logs",
					Target:      "/logs",
					BindOptions: &mount.BindOptions{},
				},
			},
			CapabilityAdd: []string{"NET_ADMIN"},
		},
	}}}
}

func TestCheckMounts(t *testing.T) {
	socket := mount.Mount{
		Type:   mount.TypeBind,
		Source: "/var/run/docker.sock",
		Target: "/var/run/docker.sock",
	}
	existing := mount.Mount{Type: mount.TypeBind, Source: "/srv/data", Target: "/data"}
	widened := mount.Mount{Type: mount.TypeBind, Source: "/", Target: "/data"}
	volume := mount.Mount{Type: mount.TypeVolume, Source: "cache", Target: "/cache"}
	pipe := mount.Mount{
		Type:   mount.TypeNamedPipe,
		Source: `\\.\pipe\docker_engine`,
		Target: `\\.\pipe\docker_engine`,
	}
	boundVolume := mount.Mount{
		Type:   mount.TypeVolume,
		Source: "root",
		Target: "/host",
		VolumeOptions: &mount.VolumeOptions{DriverConfig: &mount.Driver{
			Name:    "local",
			Options: map[string]string{"type": "none", "o": "bind", "device": "/"},
		}},
	}
	optionless := mount.Mount{Type: mount.TypeBind, Source: "/srv/logs", Target: "/logs"}

	cases := []struct {
		name    string
		mounts  []mount.Mount
		level   config.OperationsLevel
		refused bool
	}{
		{"new bind at tier 2", []mount.Mount{socket}, config.OpsConfiguration, true},
		{"changed bind at tier 2", []mount.Mount{widened}, config.OpsConfiguration, true},
		{"new bind at tier 3", []mount.Mount{socket}, config.OpsImpactful, false},
		{
			"unchanged bind and a volume at tier 2",
			[]mount.Mount{existing, volume},
			config.OpsConfiguration,
			false,
		},
		{"bind removed at tier 2", nil, config.OpsConfiguration, false},
		{"named pipe at tier 2", []mount.Mount{pipe}, config.OpsConfiguration, true},
		{
			"volume with a driver at tier 2",
			[]mount.Mount{boundVolume},
			config.OpsConfiguration,
			true,
		},
		{"volume with a driver at tier 3", []mount.Mount{boundVolume}, config.OpsImpactful, false},
		{"bind sent without options", []mount.Mount{optionless}, config.OpsConfiguration, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckMounts(hostAccessService(), tc.mounts, tc.level)

			var denied *HostAccessError
			if refused := errors.As(err, &denied); refused != tc.refused {
				t.Errorf("err = %v, want refused=%v", err, tc.refused)
			}
		})
	}
}

func TestCheckCapabilities(t *testing.T) {
	if err := CheckCapabilities(
		hostAccessService(),
		[]string{"NET_ADMIN"},
		config.OpsConfiguration,
	); err != nil {
		t.Errorf("kept capability refused: %v", err)
	}
	if err := CheckCapabilities(
		hostAccessService(),
		[]string{"NET_ADMIN", "SYS_ADMIN"},
		config.OpsConfiguration,
	); err == nil {
		t.Error("added capability admitted at tier 2")
	}
	if err := CheckCapabilities(
		hostAccessService(),
		[]string{"SYS_ADMIN"},
		config.OpsImpactful,
	); err != nil {
		t.Errorf("added capability refused at tier 3: %v", err)
	}
}
