package compose

import (
	"testing"

	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
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
