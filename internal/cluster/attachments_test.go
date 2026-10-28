package cluster

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

// Both transports attach through these, so the defaults are where a secret or
// config lands whichever one the caller used, and where Docker's CLI puts it.
func TestReferencesDefaultToTheCLIsTargets(t *testing.T) {
	sec := SecretReference(swarm.Secret{
		ID: "s1", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "db"}},
	}, "")
	if sec.SecretID != "s1" || sec.SecretName != "db" || sec.File.Name != "/run/secrets/db" ||
		sec.File.Mode != 0o444 {
		t.Errorf("SecretReference = %+v, file %+v", sec, sec.File)
	}

	cfg := ConfigReference(swarm.Config{
		ID: "c1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "nginx"}},
	}, "")
	if cfg.ConfigID != "c1" || cfg.File.Name != "/nginx" || cfg.File.Mode != 0o444 {
		t.Errorf("ConfigReference = %+v, file %+v", cfg, cfg.File)
	}

	if got := ConfigReference(swarm.Config{}, "/etc/app.conf").File.Name; got != "/etc/app.conf" {
		t.Errorf("an explicit target became %q", got)
	}
}
