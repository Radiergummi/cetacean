package api

import (
	"context"
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
