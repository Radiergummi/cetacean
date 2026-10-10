package mcp

import (
	"fmt"
	"net/http"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// as stamps an identity on every request, standing in for the auth guard a
// test server built with auth mode "none" does not run.
func as(handler http.Handler, subject string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := &auth.Identity{Subject: subject, Provider: "headers"}
		handler.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
	})
}

// A task ID reaching another identity must tell it nothing and let it change
// nothing: every tasks/* method answers it exactly as for an ID never issued.
func TestTaskIsInvisibleToAnotherIdentity(t *testing.T) {
	c := cache.New(nil)
	seedService(t, c, "web", 3, 1)

	handler := taskTestServer(t, c).Handler()
	alice, bob := as(handler, "alice"), as(handler, "bob")

	handle := callAsTask(t, alice, "scale_service", map[string]any{
		"id":       "web",
		"replicas": float64(3),
	})

	for _, method := range []string{"tasks/get", "tasks/result", "tasks/cancel"} {
		t.Run(method, func(t *testing.T) {
			_, unknown := mcpModern(t, bob, 1, method, `{"taskId":"no-such-task"}`)
			_, foreign := mcpModern(t, bob, 1, method,
				fmt.Sprintf(`{"taskId":%q}`, handle.Task.TaskId))

			if unknown.Error == nil {
				t.Fatalf("unknown task answered without an error: %s", unknown.Result)
			}

			if foreign.Error == nil || *foreign.Error != *unknown.Error {
				t.Fatalf("another identity's task: got %+v (result %s), want %+v",
					foreign.Error, foreign.Result, unknown.Error)
			}
		})
	}

	got := pollTask(t, alice, handle.Task.TaskId)
	if got.Task.Status != string(mcplib.TaskStatusWorking) {
		t.Fatalf("owner sees %q after another identity's cancel, want %q",
			got.Task.Status, mcplib.TaskStatusWorking)
	}
}
