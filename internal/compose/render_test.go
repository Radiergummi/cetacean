package compose

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRenderLeadsWithTheFixedHeader(t *testing.T) {
	out, err := Render(File{Services: map[string]Service{"api": {Image: "nginx:1.27"}}}, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	got := string(out)
	if !strings.HasPrefix(got, "# Exported from Cetacean.") {
		t.Errorf("document does not open with the header:\n%s", got)
	}
	if !strings.Contains(got, "secrets and configs are referenced, never exported") {
		t.Error("header must say what is not in the file")
	}
	if !strings.Contains(got, "image: nginx:1.27") {
		t.Errorf("document lost the service:\n%s", got)
	}
}

func TestRenderAppendsWarningsToTheHeader(t *testing.T) {
	out, err := Render(File{}, []string{"service web: custom seccomp profile dropped"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if !strings.Contains(string(out), "# service web: custom seccomp profile dropped") {
		t.Errorf("warning not rendered as a comment:\n%s", out)
	}
}

func TestRenderOmitsTheWarningBlockWhenThereAreNone(t *testing.T) {
	out, err := Render(File{}, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if strings.Contains(string(out), "# Not carried across") {
		t.Errorf("empty warning block rendered:\n%s", out)
	}
}

// A warning quotes values a service writer chooses, such as a mount target. A
// line break in one would end the comment and put YAML in the document; a
// control character would leave a document no loader accepts.
func TestRenderKeepsWarningsInsideTheirComment(t *testing.T) {
	hostile := "/data\n...\n---\nservices:\n  evil:\n    image: attacker/evil\r\u2028\u0085\x1b\xff"
	out, err := Render(File{Services: map[string]Service{"api": {Image: "nginx:1.27"}}},
		[]string{"mount " + hostile + " dropped"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	var got File
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatalf("export does not load: %v\n%q", err, out)
	}
	if len(got.Services) != 1 || got.Services["api"].Image != "nginx:1.27" {
		t.Errorf("warning changed the document: %+v\n%s", got.Services, out)
	}
	if strings.ContainsAny(string(out), "\r\u2028\u0085") {
		t.Errorf("a line break survived in a comment:\n%q", out)
	}
}
