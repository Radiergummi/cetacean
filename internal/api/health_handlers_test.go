package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
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

type timedOutResyncer struct{}

func (timedOutResyncer) Resync(context.Context) error {
	return fmt.Errorf("full sync: %w", context.DeadlineExceeded)
}

// The shared sweep ran out of time while this caller was still waiting.
func TestHandleResyncAnswersATimedOutSweepWith504(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/-/resync", nil)
	rec := httptest.NewRecorder()

	HandleResync(timedOutResyncer{})(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusGatewayTimeout)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
}
