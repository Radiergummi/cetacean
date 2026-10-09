package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/radiergummi/cetacean/internal/config"
)

func TestHealthcheckURL(t *testing.T) {
	cases := []struct {
		listen, basePath string
		tls              bool
		want             string
	}{
		{":9000", "", false, "http://localhost:9000/-/ready"},
		{"0.0.0.0:9000", "", false, "http://localhost:9000/-/ready"},
		{"[::]:9000", "/cetacean", false, "http://localhost:9000/cetacean/-/ready"},
		{"127.0.0.1:9000", "", true, "https://127.0.0.1:9000/-/ready"},
		{"[::1]:9000", "", false, "http://[::1]:9000/-/ready"},
	}
	for _, tc := range cases {
		got, err := healthcheckURL(tc.listen, tc.basePath, tc.tls)
		if err != nil {
			t.Errorf("%q: %v", tc.listen, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.listen, got, tc.want)
		}
	}
}

// The probe must find the server wherever the configuration put it, including
// settings that come only from the config file.
func TestRunHealthcheckReadsTheConfigFile(t *testing.T) {
	t.Setenv("CETACEAN_LISTEN_ADDR", "")
	t.Setenv("CETACEAN_BASE_PATH", "")

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewUnstartedServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/cetacean/-/ready" {
				http.NotFound(w, r)
				return
			}
		}),
	)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)

	path := filepath.Join(t.TempDir(), "cetacean.toml")
	toml := fmt.Sprintf("[server]\nlisten_addr = %q\nbase_path = \"/cetacean\"\n", listener.Addr())
	if err := os.WriteFile(path, []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}

	if code := runHealthcheck(&config.Flags{Config: path}); code != 0 {
		t.Errorf("healthcheck exit = %d, want 0", code)
	}
}

func TestProbeReadyOverTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(server.Close)

	if err := probeReady(server.URL+"/-/ready", true); err != nil {
		t.Errorf("probe over TLS: %v", err)
	}
}
