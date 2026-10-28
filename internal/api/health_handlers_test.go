package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/api/problem"
)

type cancelledResyncer struct{}

func (cancelledResyncer) Resync(ctx context.Context) error { return ctx.Err() }

// A caller that gave up before the shared sync returned must not be told the
// resync failed — the sync is detached and may still succeed for whoever is
// left waiting on it.
func TestHandleResyncIgnoresACancelledCaller(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodPost, "/-/resync", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	HandleResync(cancelledResyncer{})(rec, req)

	if rec.Body.Len() != 0 {
		t.Errorf("wrote a response to a cancelled caller: %q", rec.Body.String())
	}
}

type failingResyncer struct{ err error }

func (r failingResyncer) Resync(context.Context) error { return r.err }

func TestHandleResyncAnswersAFailedSweepWithProblemDetails(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"timed out", fmt.Errorf("listing tasks: %w", context.DeadlineExceeded), 504, "ENG005"},
		{"cancelled at shutdown", fmt.Errorf("listing tasks: %w", context.Canceled), 502, "ENG006"},
		{"refused", errors.New("swarm must have a leader"), 502, "ENG006"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, "/-/resync", nil)
		rec := httptest.NewRecorder()

		HandleResync(failingResyncer{tc.err})(rec, req)

		var problem problem.Detail
		if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
			t.Fatalf("%s: body is not JSON: %v", tc.name, err)
		}
		if rec.Code != tc.status || problem.Status != tc.status {
			t.Errorf(
				"%s: status = %d (body %d), want %d",
				tc.name,
				rec.Code,
				problem.Status,
				tc.status,
			)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("%s: Content-Type = %q, want application/problem+json", tc.name, ct)
		}
		if !strings.HasSuffix(problem.Type, "/api/errors/"+tc.code) {
			t.Errorf("%s: type = %q, want the %s error", tc.name, problem.Type, tc.code)
		}
		if problem.Detail != tc.err.Error() {
			t.Errorf("%s: detail = %q, want %q", tc.name, problem.Detail, tc.err.Error())
		}
	}
}
