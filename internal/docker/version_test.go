package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

// versionDaemon serves every resource at Version 7 and records the version
// each update names.
func versionDaemon(t *testing.T) (*Client, func() []string) {
	t.Helper()

	var (
		mu      sync.Mutex
		written []string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path[strings.Index(r.URL.Path[1:], "/")+1:]

		if r.Method == http.MethodPost && strings.HasSuffix(path, "/update") {
			mu.Lock()
			written = append(written, r.URL.Query().Get("version"))
			mu.Unlock()
			_, _ = w.Write([]byte(`{}`))

			return
		}

		meta := swarm.Meta{Version: swarm.Version{Index: 7}}
		var body any
		switch {
		case strings.HasPrefix(path, "/services/"):
			body = swarm.Service{ID: "svc", Meta: meta, Spec: swarm.ServiceSpec{
				TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{}},
			}}
		case strings.HasPrefix(path, "/nodes/"):
			body = swarm.Node{ID: "node", Meta: meta}
		case strings.HasPrefix(path, "/configs/"):
			body = swarm.Config{ID: "cfg", Meta: meta}
		case strings.HasPrefix(path, "/secrets/"):
			body = swarm.Secret{ID: "sec", Meta: meta}
		default:
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)

	client, err := NewClient("tcp://" + strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	return client, func() []string {
		mu.Lock()
		defer mu.Unlock()

		return append([]string(nil), written...)
	}
}

// A write a precondition pinned must name the validated version, not the one
// it reads, or a change landing in between is overwritten instead of refused.
func TestWritesNameThePinnedVersion(t *testing.T) {
	keep := func(m map[string]string) (map[string]string, error) { return m, nil }

	writes := []struct {
		kind, id string
		write    func(*Client, context.Context) error
	}{
		{"service", "svc", func(c *Client, ctx context.Context) error {
			_, err := c.UpdateServiceEnv(ctx, "svc", keep)
			return err
		}},
		{"service", "svc", func(c *Client, ctx context.Context) error {
			_, err := c.UpdateServiceSpec(ctx, "svc", func(*swarm.ServiceSpec) error { return nil })
			return err
		}},
		{"node", "node", func(c *Client, ctx context.Context) error {
			_, err := c.UpdateNodeLabels(ctx, "node", keep)
			return err
		}},
		{"config", "cfg", func(c *Client, ctx context.Context) error {
			_, err := c.UpdateConfigLabels(ctx, "cfg", keep)
			return err
		}},
		{"secret", "sec", func(c *Client, ctx context.Context) error {
			_, err := c.UpdateSecretLabels(ctx, "sec", keep)
			return err
		}},
	}

	for _, w := range writes {
		t.Run(w.kind, func(t *testing.T) {
			client, written := versionDaemon(t)
			pin := swarm.Version{Index: 5}

			contexts := []context.Context{
				WithPinnedVersion(context.Background(), w.kind, w.id, pin),
				context.Background(),
				WithPinnedVersion(context.Background(), w.kind, "other", pin),
			}
			for _, ctx := range contexts {
				if err := w.write(client, ctx); err != nil {
					t.Fatalf("write: %v", err)
				}
			}

			got := strings.Join(written(), ",")
			if got != "5,7,7" {
				t.Errorf("versions written = %s, want 5 when pinned, else the one read (7)", got)
			}
		})
	}
}
