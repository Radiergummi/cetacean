package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
)

// The service list carries task and node counts, so its stream carries what
// moves them, and replays it after a reconnect like its own events.
func TestServiceListStreamCarriesWhatMovesItsCounts(t *testing.T) {
	router, c, broadcaster := streamTestRouter(t, 0)

	seen := c.History().Count()
	c.SetService(swarm.Service{ID: "svc-stream"})
	c.SetTask(swarm.Task{ID: "task-stream", ServiceID: "svc-stream"})
	c.SetNode(swarm.Node{ID: "node-stream"})
	c.SetConfig(swarm.Config{ID: "config-stream"})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	req := httptest.NewRequestWithContext(ctx, "GET", "/services", nil)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Last-Event-ID", broadcaster.EventID(seen))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, id := range []string{"svc-stream", "task-stream", "node-stream"} {
		if !strings.Contains(body, `"id":"`+id+`"`) {
			t.Errorf("replay = %q, want the event of %s", body, id)
		}
	}

	if strings.Contains(body, `"id":"config-stream"`) {
		t.Errorf("replay = %q, carries a config event", body)
	}
}
