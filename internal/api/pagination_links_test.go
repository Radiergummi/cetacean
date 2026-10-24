package api

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

func TestAnOffsetPastTheEndLinksNoNextPage(t *testing.T) {
	for _, offset := range []int{3, 4, math.MaxInt} {
		t.Run(strconv.Itoa(offset), func(t *testing.T) {
			w := httptest.NewRecorder()
			writePaginationLinks(
				w,
				httptest.NewRequest(http.MethodGet, "/nodes", nil),
				3,
				2,
				offset,
			)

			link := w.Header().Get("Link")
			if strings.Contains(link, `rel="next"`) {
				t.Errorf("Link = %q, want no next page", link)
			}
			if strings.Contains(link, "offset=-") {
				t.Errorf("Link = %q names a negative offset", link)
			}
			if !strings.Contains(link, "offset=1>") {
				t.Errorf("Link = %q, want prev to reach the last page", link)
			}
		})
	}
}

// The suffix is what named the representation, so a page link without it is
// answered from an Accept that may say something else.
func TestPageLinksKeepTheSuffix(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	c.SetNode(swarm.Node{ID: "n2"})
	router := newTestRouterWithCache(t, c)

	for _, path := range []string{"/nodes.json?limit=1", "/history.json?limit=1"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))

			link := strings.Join(w.Header().Values("Link"), ", ")
			prefix, _, _ := strings.Cut(path, "?")
			if !strings.Contains(link, "<"+prefix+"?") || !strings.Contains(link, `rel="next"`) {
				t.Errorf("Link = %q, want a next page at %s", link, prefix)
			}
		})
	}
}
