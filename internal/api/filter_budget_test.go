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

func TestListFilterHasARequestBudget(t *testing.T) {
	for _, tc := range []struct {
		name       string
		items      int
		expression string
	}{
		{"cheap per item, but many items", 1000, `filter(1..100000, {# % 7 == 0}) != nil`},
		{
			"one item, one long run",
			1,
			`len(reduce(1..20000, #acc + "` + strings.Repeat("a", 300) + `", "")) > 0`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cache.New(nil)
			for i := range tc.items {
				c.SetConfig(swarm.Config{
					ID: fmt.Sprintf("cfg%d", i),
					Spec: swarm.ConfigSpec{
						Annotations: swarm.Annotations{Name: fmt.Sprintf("cfg%d", i)},
					},
				})
			}
			router := newTestRouterWithCache(t, c)

			hostile := url.QueryEscape(tc.expression)
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
		})
	}
}
