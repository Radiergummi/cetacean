package sbom

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEveryComponentResolvesToATextInThePool(t *testing.T) {
	// Guards against a half-regenerated pair of artifacts: a projection that
	// references a text id the pool no longer holds would render an empty
	// license dialog in production and nowhere else.
	doc, err := Project(Raw())
	if err != nil {
		t.Fatalf("Project: %v", err)
	}

	var missing []string
	for _, component := range doc.Components {
		if component.Ecosystem == "other" {
			continue
		}

		if component.TextID == "" {
			missing = append(missing, component.Name+" (no textId)")

			continue
		}

		if _, ok := Text(component.TextID); !ok {
			missing = append(missing, component.Name+" -> "+component.TextID)
		}

		if component.NoticeID != "" {
			if _, ok := Text(component.NoticeID); !ok {
				missing = append(missing, component.Name+" notice -> "+component.NoticeID)
			}
		}
	}

	if len(missing) > 0 {
		t.Fatalf(
			"%d components do not resolve; run 'make sbom':\n  %s",
			len(missing), strings.Join(missing, "\n  "),
		)
	}
}

func TestTextRejectsUnknownID(t *testing.T) {
	if _, ok := Text("0000000000000000000000000000000000000000"); ok {
		t.Error("unknown id resolved to a text")
	}
}

func TestNoticesArePresentForApacheComponents(t *testing.T) {
	// docker/docker ships a NOTICE, and Apache-2.0 section 4(d) makes carrying
	// it mandatory. If this stops holding, the harvester stopped finding them.
	doc, err := Project(Raw())
	if err != nil {
		t.Fatalf("Project: %v", err)
	}

	for _, component := range doc.Components {
		if component.Name == "github.com/docker/docker" {
			if component.NoticeID == "" {
				t.Fatal("github.com/docker/docker has a NOTICE file but no noticeId")
			}

			return
		}
	}

	t.Skip("github.com/docker/docker is no longer a dependency")
}

func TestProjectedJSONHasPopulatedTextIDs(t *testing.T) {
	// Critical assertion: ProjectedJSON() is the cached document served at
	// GET /-/licenses. It must have all text IDs populated. Unlike
	// TestEveryComponentResolvesToATextInThePool (which exercises a fresh
	// Project() call after all inits), this validates what the server actually
	// returns. Catches init-order bugs or any refactor that drops ids from
	// most components while leaving a few (still passes "at least one" logic).
	var doc Document
	if err := json.Unmarshal(ProjectedJSON(), &doc); err != nil {
		t.Fatalf("unmarshal ProjectedJSON: %v", err)
	}

	var missing []string
	for _, component := range doc.Components {
		if component.Ecosystem == "other" {
			continue
		}

		if component.TextID == "" {
			missing = append(missing, component.Name)

			continue
		}

		if _, ok := Text(component.TextID); !ok {
			missing = append(missing, component.Name+" -> unresolvable "+component.TextID)
		}

		if component.NoticeID != "" {
			if _, ok := Text(component.NoticeID); !ok {
				noticeMsg := component.Name + " notice -> unresolvable " +
					component.NoticeID
				missing = append(missing, noticeMsg)
			}
		}
	}

	if len(missing) > 0 {
		t.Fatalf(
			"ProjectedJSON: %d components missing or unresolvable text ids:\n  %s",
			len(missing), strings.Join(missing[:min(len(missing), 5)], "\n  "),
		)
	}
}
