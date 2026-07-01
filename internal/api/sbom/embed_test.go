package sbom

import (
	"encoding/json"
	"testing"
)

func TestRawIsValidCycloneDX(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(Raw(), &doc); err != nil {
		t.Fatalf("embedded SBOM is not valid JSON: %v", err)
	}
	if doc["bomFormat"] != "CycloneDX" {
		t.Errorf("bomFormat = %v, want CycloneDX", doc["bomFormat"])
	}
}

func TestProjectedJSONHasComponents(t *testing.T) {
	var doc Document
	if err := json.Unmarshal(ProjectedJSON(), &doc); err != nil {
		t.Fatalf("projected JSON invalid: %v", err)
	}
	if len(doc.Components) == 0 {
		t.Error("expected at least one projected component from the committed SBOM")
	}
}
