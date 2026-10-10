package config

import (
	"strings"
	"testing"
)

func TestLoad_DockerTLSFromEachSource(t *testing.T) {
	t.Setenv("CETACEAN_DOCKER_HOST", "tcp://engine:2376")
	t.Setenv("CETACEAN_DOCKER_TLS_CERT", "/env/cert.pem")
	t.Setenv("CETACEAN_DOCKER_TLS_KEY", "/env/key.pem")

	flags, err := ParseFlags([]string{"-docker-tls-cert", "/flag/cert.pem"})
	if err != nil {
		t.Fatal(err)
	}

	fc := &fileConfig{Docker: &fileDocker{TLS: &fileDockerTLS{
		CA:  new("/file/ca.pem"),
		Key: new("/file/key.pem"),
	}}}

	cfg, err := Load(fc, flags)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := DockerTLSConfig{CA: "/file/ca.pem", Cert: "/flag/cert.pem", Key: "/env/key.pem"}
	if cfg.DockerTLS != want {
		t.Errorf("DockerTLS = %+v, want %+v", cfg.DockerTLS, want)
	}
}

func TestLoad_RefusesADockerSetupThatCannotConnect(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "an ssh host",
			env:  map[string]string{"CETACEAN_DOCKER_HOST": "ssh://admin@engine"},
			want: "ssh://",
		},
		{
			name: "TLS to a Unix socket",
			env: map[string]string{
				"CETACEAN_DOCKER_HOST":   "unix:///var/run/docker.sock",
				"CETACEAN_DOCKER_TLS_CA": "/ca.pem",
			},
			want: "docker.tls",
		},
		{
			name: "a client certificate without its key",
			env: map[string]string{
				"CETACEAN_DOCKER_HOST":     "tcp://engine:2376",
				"CETACEAN_DOCKER_TLS_CERT": "/cert.pem",
			},
			want: "docker.tls.key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			_, err := Load(nil, nil)
			if err == nil {
				t.Fatal("Load accepted it")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error does not name %s: %v", tt.want, err)
			}
		})
	}
}
