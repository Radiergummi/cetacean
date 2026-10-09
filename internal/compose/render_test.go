package compose

import (
	"strings"
	"testing"
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
// line break in one would end the comment and put YAML in the document.
func TestRenderKeepsWarningsInsideTheirComment(t *testing.T) {
	hostile := "/data\n...\n---\nservices:\n  evil:\n    image: attacker/evil\r x"
	out, err := Render(File{Services: map[string]Service{"api": {Image: "nginx:1.27"}}},
		[]string{"mount " + hostile + " dropped"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.Contains(line, "attacker/evil") && !strings.HasPrefix(line, "#") {
			t.Fatalf("warning escaped its comment:\n%s", out)
		}
	}
	if strings.Count(string(out), "\n---") != 0 || strings.Contains(string(out), " ") {
		t.Errorf("a document marker or line separator survived:\n%q", out)
	}
}
