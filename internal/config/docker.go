package config

import (
	"errors"
	"fmt"
	"strings"
)

// DockerTLSConfig names the PEM files for reaching the engine over tcp:// with
// TLS. An empty CA trusts the system roots.
type DockerTLSConfig struct {
	CA   string
	Cert string
	Key  string
}

func (c DockerTLSConfig) Enabled() bool {
	return c.CA != "" || c.Cert != "" || c.Key != ""
}

func loadDockerTLS(flags *Flags, fd *fileDocker) DockerTLSConfig {
	var ft *fileDockerTLS
	if fd != nil {
		ft = fd.TLS
	}

	return DockerTLSConfig{
		CA: resolve(
			flags.DockerTLSCA,
			"CETACEAN_DOCKER_TLS_CA",
			fileField(ft, func(t *fileDockerTLS) *string { return t.CA }),
			"",
		),
		Cert: resolve(
			flags.DockerTLSCert,
			"CETACEAN_DOCKER_TLS_CERT",
			fileField(ft, func(t *fileDockerTLS) *string { return t.Cert }),
			"",
		),
		Key: resolve(
			flags.DockerTLSKey,
			"CETACEAN_DOCKER_TLS_KEY",
			fileField(ft, func(t *fileDockerTLS) *string { return t.Key }),
			"",
		),
	}
}

// validateDocker refuses an ssh:// host up front: the client would dial
// "user@host" as a TCP address and fail on DNS, and the image has no ssh.
func validateDocker(host string, t DockerTLSConfig) error {
	scheme, _, _ := strings.Cut(host, "://")

	switch {
	case scheme == "ssh":
		return errors.New(
			"docker.host: ssh:// is not supported; forward the engine's socket " +
				"over SSH, or use tcp:// with docker.tls",
		)
	case t.Enabled() && scheme != "tcp":
		return fmt.Errorf("docker.tls applies only to a tcp:// docker.host, not %q", host)
	case (t.Cert == "") != (t.Key == ""):
		return errors.New("docker.tls.cert and docker.tls.key must be set together, or neither")
	}

	return nil
}
