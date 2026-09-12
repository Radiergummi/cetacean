package docker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeDaemon answers /_ping with the Api-Version header, and with a 400 that
// still carries it when the client asks for a version it does not serve.
func fakeDaemon(t *testing.T, apiVersion string) *Client {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Api-Version", apiVersion)

		// A versioned path above what it serves is refused, header still set.
		if apiVersion < RequiredAPIVersion &&
			strings.Contains(r.URL.Path, "/v"+RequiredAPIVersion+"/") {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		w.WriteHeader(http.StatusOK)
	}))

	t.Cleanup(server.Close)

	client, err := NewClient("tcp://" + strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	t.Cleanup(func() { _ = client.Close() })

	return client
}

func TestCheckAPIVersionAcceptsACurrentDaemon(t *testing.T) {
	for _, version := range []string{RequiredAPIVersion, "1.47", "1.51"} {
		t.Run(version, func(t *testing.T) {
			if err := fakeDaemon(t, version).CheckAPIVersion(context.Background()); err != nil {
				t.Errorf("daemon serving API %s was refused: %v", version, err)
			}
		})
	}
}

// TestCheckAPIVersionRefusesAnOldDaemon: without this an operator meets empty
// listings and a 503, which looks like an unmounted socket or a worker node.
func TestCheckAPIVersionRefusesAnOldDaemon(t *testing.T) {
	// 1.43 is Docker Engine 24, 1.44 is 25, 1.45 is 26 — all still deployed.
	for _, version := range []string{"1.43", "1.44", "1.45"} {
		t.Run(version, func(t *testing.T) {
			err := fakeDaemon(t, version).CheckAPIVersion(context.Background())
			if err == nil {
				t.Fatalf("daemon serving API %s was accepted", version)
			}

			var unsupported *UnsupportedEngineError
			if !errors.As(err, &unsupported) {
				t.Fatalf("error is %T, want *UnsupportedEngineError: %v", err, err)
			}

			// Naming both versions and the Engine release is what makes it
			// actionable.
			for _, want := range []string{version, RequiredAPIVersion, MinimumEngineVersion} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("message does not name %q: %s", want, err)
				}
			}
		})
	}
}

// TestCheckAPIVersionToleratesAnAbsentDaemon pins the deliberate non-failure:
// a socket not there yet is ordinary at startup, and the watcher retries.
func TestCheckAPIVersionToleratesAnAbsentDaemon(t *testing.T) {
	client, err := NewClient("tcp://127.0.0.1:1")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	t.Cleanup(func() { _ = client.Close() })

	if err := client.CheckAPIVersion(context.Background()); err != nil {
		t.Errorf("an unreachable daemon was treated as unsupported: %v", err)
	}
}
