package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// Each item's evaluation is bounded by expr's own memory budget, but a list
// runs the expression once per item, so the request needs a bound of its own.
func TestListFilterHasARequestBudget(t *testing.T) {
	c := cache.New(nil)
	for i := range 1000 {
		c.SetConfig(swarm.Config{
			ID:   fmt.Sprintf("cfg%d", i),
			Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: fmt.Sprintf("cfg%d", i)}},
		})
	}
	router := newTestRouterWithCache(t, c)

	hostile := url.QueryEscape(`filter(1..100000, {# % 7 == 0}) != nil`)
	req := httptest.NewRequest("GET", "/configs?filter="+hostile, nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	started := time.Now()
	router.ServeHTTP(w, req)
	elapsed := time.Since(started)

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "FLT004") {
		t.Errorf("status = %d, want 400 FLT004", w.Code)
	}
	if elapsed > 2*filterBudget+time.Second {
		t.Errorf("took %s, want about the %s budget", elapsed, filterBudget)
	}
}
