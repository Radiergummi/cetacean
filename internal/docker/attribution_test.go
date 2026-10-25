package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// attributionDaemon accepts every write, or refuses every one when refuse is
// set, and serves each resource at Version 7.
func attributionDaemon(t *testing.T, refuse bool) *Client {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path[strings.Index(r.URL.Path[1:], "/")+1:]
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodGet {
			if refuse {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"message":"update out of sequence"}`))

				return
			}

			_, _ = w.Write([]byte(`{"ID":"cfg"}`))

			return
		}

		meta := swarm.Meta{Version: swarm.Version{Index: 7}}
		switch {
		case strings.HasPrefix(path, "/services/"):
			_ = json.NewEncoder(w).Encode(swarm.Service{ID: "svc", Meta: meta})
		case strings.HasPrefix(path, "/nodes/"):
			_ = json.NewEncoder(w).Encode(swarm.Node{ID: "node", Meta: meta})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client, err := NewClient("tcp://"+strings.TrimPrefix(server.URL, "http://"), TLS{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	return client
}

var operator = cache.Actor{Subject: "u-1", Name: "alice", Provider: "oidc", Transport: "rest"}

func TestWritesAreAttributedToTheActorInTheContext(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc", Meta: swarm.Meta{Version: swarm.Version{Index: 7}}})

	client := attributionDaemon(t, false)
	client.AttributeTo(c.Attributions())

	ctx := cache.WithActor(context.Background(), operator)
	if _, err := client.ScaleService(ctx, "svc", 3); err == nil {
		t.Fatal("scaling a service without a replicated mode succeeded")
	}

	if n := c.Attributions().Len(); n != 0 {
		t.Fatalf("a write refused before reaching the engine left %d attributions", n)
	}

	if _, err := client.RestartService(ctx, "svc"); err != nil {
		t.Fatalf("RestartService: %v", err)
	}

	// The watcher observes the version the write produced.
	c.SetService(swarm.Service{ID: "svc", Meta: swarm.Meta{Version: swarm.Version{Index: 9}}})

	entries := c.History().List(cache.HistoryQuery{Limit: 1})
	if len(entries) != 1 || entries[0].Actor == nil || *entries[0].Actor != operator {
		t.Fatalf("newest entry = %+v, want it attributed to %+v", entries, operator)
	}
}

func TestEveryKindOfWriteRegistersItself(t *testing.T) {
	keep := func(m map[string]string) (map[string]string, error) { return m, nil }

	writes := map[string]func(*Client, context.Context) error{
		"node update": func(c *Client, ctx context.Context) error {
			_, err := c.UpdateNodeLabels(ctx, "node", keep)
			return err
		},
		"service remove": func(c *Client, ctx context.Context) error {
			return c.RemoveService(ctx, "svc")
		},
		"volume remove": func(c *Client, ctx context.Context) error {
			return c.RemoveVolume(ctx, "data", false)
		},
		"config create": func(c *Client, ctx context.Context) error {
			_, err := c.CreateConfig(ctx, swarm.ConfigSpec{
				Annotations: swarm.Annotations{Name: "app.conf"},
			})
			return err
		},
	}

	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			c := cache.New(nil)
			client := attributionDaemon(t, false)
			client.AttributeTo(c.Attributions())

			if err := write(client, cache.WithActor(context.Background(), operator)); err != nil {
				t.Fatalf("write: %v", err)
			}

			if n := c.Attributions().Len(); n != 1 {
				t.Fatalf("%d attributions pending, want 1", n)
			}

			if err := write(client, context.Background()); err != nil {
				t.Fatalf("write: %v", err)
			}

			if n := c.Attributions().Len(); n != 1 {
				t.Fatalf("a write with no actor registered itself: %d pending", n)
			}
		})

		t.Run(name+" refused", func(t *testing.T) {
			c := cache.New(nil)
			client := attributionDaemon(t, true)
			client.AttributeTo(c.Attributions())

			if err := write(client, cache.WithActor(context.Background(), operator)); err == nil {
				t.Fatal("the refused write succeeded")
			}

			if n := c.Attributions().Len(); n != 0 {
				t.Fatalf("a refused write left %d attributions", n)
			}
		})
	}
}
