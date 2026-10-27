package cluster

import (
	"errors"
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

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
					BindOptions: &mount.BindOptions{Propagation: mount.PropagationRShared},
				},
			},
			CapabilityAdd: []string{"NET_ADMIN"},
		},
	}}}
}

// hostAccessVolumes knows a plain local volume and one bound to the host root.
func hostAccessVolumes(name string) (volume.Volume, bool) {
	switch name {
	case "cache":
		return volume.Volume{Name: name, Driver: "local"}, true
	case "hostroot":
		return volume.Volume{
			Name:    name,
			Driver:  "local",
			Options: map[string]string{"type": "none", "o": "bind", "device": "/"},
		}, true
	default:
		return volume.Volume{}, false
	}
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
	hostroot := mount.Mount{Type: mount.TypeVolume, Source: "hostroot", Target: "/h"}
	unknown := mount.Mount{Type: mount.TypeVolume, Source: "elsewhere", Target: "/e"}
	anonymous := mount.Mount{Type: mount.TypeVolume, Target: "/scratch"}
	emptyOptions := existing
	emptyOptions.BindOptions = &mount.BindOptions{}
	optionless := mount.Mount{Type: mount.TypeBind, Source: "/srv/logs", Target: "/logs"}
	spelled := func(m mount.Mount, t mount.Type) []mount.Mount {
		m.Type = t
		return []mount.Mount{m}
	}

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
		{
			"bind sent with an empty options block",
			[]mount.Mount{emptyOptions},
			config.OpsConfiguration,
			false,
		},
		{
			"bind stored with propagation, sent without",
			[]mount.Mount{optionless},
			config.OpsConfiguration,
			true,
		},
		{"untyped mount at tier 2", spelled(socket, ""), config.OpsConfiguration, true},
		{"uppercase bind at tier 2", spelled(socket, "BIND"), config.OpsConfiguration, true},
		{"capitalised bind at tier 2", spelled(socket, "Bind"), config.OpsConfiguration, true},
		{"uppercase pipe at tier 2", spelled(pipe, "NPIPE"), config.OpsConfiguration, true},
		{
			"uppercase volume at tier 2",
			spelled(boundVolume, "VOLUME"),
			config.OpsConfiguration,
			true,
		},
		{
			"bind-backed named volume at tier 2",
			[]mount.Mount{hostroot},
			config.OpsConfiguration,
			true,
		},
		{"bind-backed named volume at tier 3", []mount.Mount{hostroot}, config.OpsImpactful, false},
		{"unknown named volume at tier 2", []mount.Mount{unknown}, config.OpsConfiguration, true},
		{"anonymous volume at tier 2", []mount.Mount{anonymous}, config.OpsConfiguration, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckMounts(hostAccessService(), tc.mounts, hostAccessVolumes, tc.level)

			var denied *HostAccessError
			if refused := errors.As(err, &denied); refused != tc.refused {
				t.Errorf("err = %v, want refused=%v", err, tc.refused)
			}
		})
	}
}

func TestCheckMountsNamesTheReason(t *testing.T) {
	cases := []struct {
		mount mount.Mount
		want  string
	}{
		{
			mount.Mount{Source: "/var/run/docker.sock", Target: "/s"},
			"a bind mount of /var/run/docker.sock requires operations level 3",
		},
		{
			mount.Mount{Type: "NPIPE", Source: `\\.\pipe\x`, Target: "/p"},
			`a named pipe mount of \\.\pipe\x requires operations level 3`,
		},
		{
			mount.Mount{
				Type:          mount.TypeVolume,
				Source:        "root",
				Target:        "/host",
				VolumeOptions: &mount.VolumeOptions{DriverConfig: &mount.Driver{Name: "local"}},
			},
			"a volume mount of root with driver options requires operations level 3",
		},
		{
			mount.Mount{
				Type:          mount.TypeVolume,
				Target:        "/host",
				VolumeOptions: &mount.VolumeOptions{DriverConfig: &mount.Driver{Name: "local"}},
			},
			"an anonymous volume mount with driver options requires operations level 3",
		},
		{
			mount.Mount{Type: mount.TypeVolume, Source: "hostroot", Target: "/h"},
			"a volume mount of hostroot with driver options requires operations level 3",
		},
		{
			mount.Mount{Type: mount.TypeVolume, Source: "elsewhere", Target: "/e"},
			"a mount of unknown volume elsewhere requires operations level 3",
		},
	}
	for _, tc := range cases {
		err := CheckMounts(
			hostAccessService(),
			[]mount.Mount{tc.mount},
			hostAccessVolumes,
			config.OpsConfiguration,
		)
		if err == nil || err.Error() != tc.want {
			t.Errorf("err = %v, want %q", err, tc.want)
		}
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

func TestCheckHostAccessHeld(t *testing.T) {
	plain := swarm.ServiceSpec{TaskTemplate: swarm.TaskSpec{
		ContainerSpec: &swarm.ContainerSpec{
			Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: "cache", Target: "/c"}},
		},
	}}
	socket := swarm.ServiceSpec{TaskTemplate: swarm.TaskSpec{
		ContainerSpec: &swarm.ContainerSpec{Mounts: []mount.Mount{{
			Type:   mount.TypeBind,
			Source: "/var/run/docker.sock",
			Target: "/var/run/docker.sock",
		}}},
	}}
	capable := swarm.ServiceSpec{TaskTemplate: swarm.TaskSpec{
		ContainerSpec: &swarm.ContainerSpec{CapabilityAdd: []string{"SYS_ADMIN"}},
	}}
	elsewhere := swarm.ServiceSpec{TaskTemplate: swarm.TaskSpec{
		ContainerSpec: &swarm.ContainerSpec{
			Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: "elsewhere", Target: "/e"}},
		},
	}}
	hostroot := swarm.ServiceSpec{TaskTemplate: swarm.TaskSpec{
		ContainerSpec: &swarm.ContainerSpec{
			Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: "hostroot", Target: "/h"}},
		},
	}}

	cases := []struct {
		name    string
		svc     swarm.Service
		level   config.OperationsLevel
		refused bool
	}{
		{"no host access at tier 1", swarm.Service{Spec: plain}, config.OpsOperational, false},
		{"no container spec at tier 1", swarm.Service{}, config.OpsOperational, false},
		{"current bind at tier 1", swarm.Service{Spec: socket}, config.OpsOperational, true},
		{"current bind at tier 2", swarm.Service{Spec: socket}, config.OpsConfiguration, true},
		{"current bind at tier 3", swarm.Service{Spec: socket}, config.OpsImpactful, false},
		{
			"current capability at tier 2",
			swarm.Service{Spec: capable},
			config.OpsConfiguration,
			true,
		},
		{"unknown volume at tier 1", swarm.Service{Spec: elsewhere}, config.OpsOperational, false},
		{
			"bind-backed volume at tier 1",
			swarm.Service{Spec: hostroot},
			config.OpsOperational,
			true,
		},
		{
			"bind only in the previous spec at tier 1",
			swarm.Service{Spec: plain, PreviousSpec: &socket},
			config.OpsOperational,
			true,
		},
		{
			"bind only in the previous spec at tier 3",
			swarm.Service{Spec: plain, PreviousSpec: &socket},
			config.OpsImpactful,
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckHostAccessHeld(tc.svc, "changing the image of", hostAccessVolumes, tc.level)

			var denied *HostAccessError
			if refused := errors.As(err, &denied); refused != tc.refused {
				t.Errorf("err = %v, want refused=%v", err, tc.refused)
			}
		})
	}

	err := CheckHostAccessHeld(
		swarm.Service{Spec: socket},
		"changing the image of",
		hostAccessVolumes,
		config.OpsOperational,
	)
	want := "changing the image of a service with a bind mount of /var/run/docker.sock " +
		"requires operations level 3"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}
