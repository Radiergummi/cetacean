package compose

import (
	"testing"

	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
)

// Parsing the emitted YAML with the library the Docker CLI uses is what
// actually defends the promise that the file redeploys: every field compose-go
// rejects is one a real docker stack deploy would have rejected.
func TestEmittedDocumentParsesAsCompose(t *testing.T) {
	f, warnings := FromStack(testStack(), clusterNetworks())
	out, err := Render(f, warnings)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	project, err := loader.LoadWithContext(t.Context(), types.ConfigDetails{
		ConfigFiles: []types.ConfigFile{{Filename: "compose.yaml", Content: out}},
		Environment: types.NewMapping(nil),
	}, func(o *loader.Options) {
		o.SetProjectName("web", true)
		o.SkipNormalization = false
		o.SkipConsistencyCheck = false
	})
	if err != nil {
		t.Fatalf("compose-go rejected the document: %v\n%s", err, out)
	}

	svc, err := project.GetService("api")
	if err != nil {
		t.Fatalf("service api missing from the parsed project: %v", err)
	}

	if svc.Image != "nginx:1.27@sha256:abc" {
		t.Errorf("image round-tripped as %q", svc.Image)
	}
	if svc.Deploy == nil || svc.Deploy.Replicas == nil || *svc.Deploy.Replicas != 3 {
		t.Errorf("replicas round-tripped as %+v", svc.Deploy)
	}
	if got := svc.Environment["PORT"]; got == nil || *got != "80" {
		t.Errorf("PORT round-tripped as %v", got)
	}
	if len(svc.Ports) != 1 || svc.Ports[0].Target != 80 || svc.Ports[0].Published != "8080" {
		t.Errorf("ports round-tripped as %+v", svc.Ports)
	}
	if svc.HealthCheck == nil {
		t.Error("healthcheck did not survive the round trip")
	}
}

// A one-service export must parse on its own terms too — everything external,
// nothing declared.
func TestEmittedServiceDocumentParsesAsCompose(t *testing.T) {
	f, warnings := FromService(testService(), clusterNetworks())
	out, err := Render(f, warnings)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if _, err := loader.LoadWithContext(t.Context(), types.ConfigDetails{
		ConfigFiles: []types.ConfigFile{{Filename: "compose.yaml", Content: out}},
		Environment: types.NewMapping(nil),
	}, func(o *loader.Options) { o.SetProjectName("web", true) }); err != nil {
		t.Fatalf("compose-go rejected the single-service document: %v\n%s", err, out)
	}
}

// The mount options and ulimits are only worth carrying if compose accepts
// them where the projection puts them.
func TestEmittedMountOptionsParseAsCompose(t *testing.T) {
	svc := testService()
	c := svc.Spec.TaskTemplate.ContainerSpec
	c.Mounts = []mount.Mount{
		{
			Type:          mount.TypeVolume,
			Source:        "web_data",
			Target:        "/data",
			VolumeOptions: &mount.VolumeOptions{NoCopy: true, Subpath: "inner"},
		},
		{
			Type:        mount.TypeBind,
			Source:      "/mnt/host",
			Target:      "/mnt",
			BindOptions: &mount.BindOptions{Propagation: mount.PropagationRShared},
		},
		{
			Type:         mount.TypeTmpfs,
			Target:       "/run",
			TmpfsOptions: &mount.TmpfsOptions{SizeBytes: 1 << 20, Mode: 0o1777},
		},
	}
	c.Ulimits = []*container.Ulimit{{Name: "nofile", Soft: 20000, Hard: 40000}}

	f, warnings := FromService(svc, clusterNetworks())
	out, err := Render(f, warnings)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	project, err := loader.LoadWithContext(t.Context(), types.ConfigDetails{
		ConfigFiles: []types.ConfigFile{{Filename: "compose.yaml", Content: out}},
		Environment: types.NewMapping(nil),
	}, func(o *loader.Options) { o.SetProjectName("web", true) })
	if err != nil {
		t.Fatalf("compose-go rejected the mount options: %v\n%s", err, out)
	}

	parsed, err := project.GetService("web_api")
	if err != nil {
		t.Fatalf("service missing from the parsed project: %v", err)
	}

	byTarget := make(map[string]types.ServiceVolumeConfig, len(parsed.Volumes))
	for _, v := range parsed.Volumes {
		byTarget[v.Target] = v
	}
	if v := byTarget["/data"].Volume; v == nil || !v.NoCopy || v.Subpath != "inner" {
		t.Errorf("volume options round-tripped as %+v", v)
	}
	if b := byTarget["/mnt"].Bind; b == nil || b.Propagation != "rshared" {
		t.Errorf("bind options round-tripped as %+v", b)
	}
	if tm := byTarget["/run"].Tmpfs; tm == nil || tm.Size != 1<<20 {
		t.Errorf("tmpfs options round-tripped as %+v", tm)
	}
	if u, ok := parsed.Ulimits["nofile"]; !ok || u.Soft != 20000 || u.Hard != 40000 {
		t.Errorf("ulimits round-tripped as %+v", parsed.Ulimits)
	}
}
