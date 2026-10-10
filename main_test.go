package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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

// Go reads the proxy environment once per process, so the probe runs in a
// child started with HTTP_PROXY set; a probe sent through it gets a 502.
func TestProbeReadyIgnoresTheProxyEnvironment(t *testing.T) {
	if target := os.Getenv("CETACEAN_TEST_PROBE_TARGET"); target != "" {
		if err := probeReady(target, false); err != nil {
			t.Fatal(err)
		}
		return
	}

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(proxy.Close)

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(server.Close)

	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command( //nolint:gosec // re-runs this test binary
		os.Args[0],
		"-test.run=^TestProbeReadyIgnoresTheProxyEnvironment$",
	)
	cmd.Env = append(os.Environ(),
		"HTTP_PROXY="+proxy.URL,
		"NO_PROXY=",
		"CETACEAN_TEST_PROBE_TARGET=http://localhost.:"+port+"/-/ready",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("probe went through the proxy: %v\n%s", err, out)
	}
}

func TestProbeReadyOverTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(server.Close)

	if err := probeReady(server.URL+"/-/ready", true); err != nil {
		t.Errorf("probe over TLS: %v", err)
	}
}

// In tsnet mode only the meta listener sits on server.listen_addr, so it must
// answer where the probe asks once a base path is set.
func TestHealthcheckReachesTheTsnetMetaListener(t *testing.T) {
	ok := func(http.ResponseWriter, *http.Request) {}
	server := httptest.NewServer(newMetaMux("/cetacean", ok, ok))
	t.Cleanup(server.Close)

	target, err := healthcheckURL(server.Listener.Addr().String(), "/cetacean", false)
	if err != nil {
		t.Fatal(err)
	}

	if err := probeReady(target, false); err != nil {
		t.Errorf("probe: %v", err)
	}
}
