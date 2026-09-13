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
