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
	// Critical gap: TestEveryComponentResolvesToATextInThePool exercises a fresh
	// Project() call that runs after all inits complete. But the server uses
	// ProjectedJSON(), which is computed once at init time. If init-order causes
	// Attach() to run before the text pool is ready, ProjectedJSON() will have
	// zero textId values permanently. This test catches that by asserting over
	// the actual cached document served at GET /-/licenses.
	var doc Document
	if err := json.Unmarshal(ProjectedJSON(), &doc); err != nil {
		t.Fatalf("unmarshal ProjectedJSON: %v", err)
	}

	withTextID := 0
	for _, component := range doc.Components {
		if component.Ecosystem == "other" {
			continue
		}

		if component.TextID != "" {
			withTextID++

			if _, ok := Text(component.TextID); !ok {
				t.Errorf(
					"ProjectedJSON component %s has unresolvable textId=%q",
					component.Name, component.TextID,
				)
			}
		}

		if component.NoticeID != "" {
			if _, ok := Text(component.NoticeID); !ok {
				t.Errorf(
					"ProjectedJSON component %s has unresolvable noticeId=%q",
					component.Name, component.NoticeID,
				)
			}
		}
	}

	if withTextID == 0 {
		t.Error(
			"ProjectedJSON has 0 components with textId (init-order bug: " +
				"embed.go called Project() before texts.go populated the pool)",
		)
	}
}
