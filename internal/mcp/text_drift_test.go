package mcp

import (
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
)

// finishLogRead cuts the merged read to `tail`, so a description promising
// `tail` lines per replica promises several times what a read returns.
func TestGetLogsTailDescribesTheMergedCut(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil))

	td, ok := srv.findTool("get_logs")
	if !ok {
		t.Fatal("get_logs not registered")
	}

	tail, _ := td.tool.InputSchema.Properties["tail"].(map[string]any)
	description, _ := tail["description"].(string)

	if strings.Contains(description, "per replica") {
		t.Errorf("tail says per replica: %q", description)
	}
}

// A resource read returns the digest, not the Docker record, so a prompt that
// sends the model to it for a raw spec path sends it looking for a field that
// is not there.
func TestPromptTextCitesWhatTheReadsReturn(t *testing.T) {
	digestFields := map[string][]string{
		promptTextExplainUnschedulable: {"placementConstraints", "cpuReservationCores"},
		promptTextRollBackService:      {"updateState"},
		promptTextRightSizeService:     {"cpuReservationCores", "memoryLimitBytes"},
	}
	stale := []string{
		"Spec.TaskTemplate.Placement -",
		"UpdateStatus.State",
		"Resources.Reservations",
		"Resources.Limits",
		"Status.Err",
		"<id> with raw",
	}

	for text, fields := range digestFields {
		for _, field := range fields {
			if !strings.Contains(text, field) {
				t.Errorf("prompt does not cite the digest field %q:\n%s", field, text)
			}
		}
	}

	for _, text := range []string{
		promptTextDiagnoseService, promptTextExplainUnschedulable,
		promptTextRollBackService, promptTextRightSizeService,
	} {
		for _, phrase := range stale {
			if strings.Contains(text, phrase) {
				t.Errorf("prompt cites %q, which no read returns:\n%s", phrase, text)
			}
		}
	}
}
