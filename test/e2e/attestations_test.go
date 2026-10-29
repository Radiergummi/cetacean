//go:build e2e

package e2e_test

import (
	"net/http"
	"testing"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

type provenanceBody struct {
	Status string `json:"status"`
}

// The fixture image is built locally and carries no digest, so a service
// running it has nothing to look its attestations up by — and no registry is
// asked.
func TestProvenanceOfAnUnpinnedImage(t *testing.T) {
	_, proc := startNone(t, "")

	var body provenanceBody

	path := "/services/" + serviceID(t, proc, "shop_web") + "/provenance"

	resp := getJSON(t, proc, path, &body) //nolint:bodyclose // closed in getJSON
	if resp.StatusCode != http.StatusOK || body.Status != "unpinned" {
		t.Errorf("status %d, %q; want 200, unpinned", resp.StatusCode, body.Status)
	}
}

func TestProvenanceWhenSwitchedOff(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	proc := sut.Start(t, sut.Config{
		Port:       19001,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":          "none",
			"CETACEAN_IMAGE_ATTESTATIONS": "false",
		},
	})

	var body provenanceBody

	path := "/services/" + serviceID(t, proc, "shop_web") + "/provenance"

	resp := getJSON(t, proc, path, &body) //nolint:bodyclose // closed in getJSON
	if resp.StatusCode != http.StatusOK || body.Status != "disabled" {
		t.Errorf("status %d, %q; want 200, disabled", resp.StatusCode, body.Status)
	}
}
