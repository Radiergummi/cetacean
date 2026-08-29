package sbom

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTextRejectsUnknownID(t *testing.T) {
	if _, ok := Text("0000000000000000000000000000000000000000"); ok {
		t.Error("unknown id resolved to a text")
	}
}

func TestNoticesArePresentForApacheComponents(t *testing.T) {
	// docker/docker ships a NOTICE, and Apache-2.0 section 4(d) makes carrying
	// it mandatory. If this stops holding, the harvester stopped finding them.
	doc := projectedDocument(t)

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
	// ProjectedJSON() is the cached document served at GET /-/licenses, and the
	// only one that carries text ids. Guards against a half-regenerated pair of
	// artifacts — a projection referencing an id the pool no longer holds would
	// render an empty license dialog in production and nowhere else — and
	// against a refactor that drops ids from most components but not all.
	doc := projectedDocument(t)

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

// projectedDocument decodes the document the server actually serves, which is
// the only one attachTexts has stamped with text ids.
func projectedDocument(t *testing.T) Document {
	t.Helper()

	var doc Document
	if err := json.Unmarshal(ProjectedJSON(), &doc); err != nil {
		t.Fatalf("unmarshal ProjectedJSON: %v", err)
	}

	return doc
}
