package api

import (
	"encoding/json"
	"html"
	"regexp"
	"testing"
)

// Scalar defaults to loading fonts from its CDN and sending telemetry; the
// playground's Content-Security-Policy blocks both, so the page turns them off.
func TestThePlaygroundTurnsOffScalarsRemoteDefaults(t *testing.T) {
	match := regexp.MustCompile(`data-configuration='([^']*)'`).
		FindStringSubmatch(apiPlaygroundHTML)
	if match == nil {
		t.Fatal("the playground carries no data-configuration")
	}

	var configuration map[string]any
	if err := json.Unmarshal([]byte(html.UnescapeString(match[1])), &configuration); err != nil {
		t.Fatalf("data-configuration is not JSON: %v", err)
	}

	for _, key := range []string{"telemetry", "withDefaultFonts"} {
		if configuration[key] != false {
			t.Errorf("%s = %v, want false", key, configuration[key])
		}
	}
}
