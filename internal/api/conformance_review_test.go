package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	promapi "github.com/radiergummi/cetacean/internal/prometheus"
	"github.com/radiergummi/cetacean/internal/spec"
)

func webService() *cache.Cache {
	replicas := uint64(1)
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
		},
	})

	return c
}

// OPTIONS and a 405 report the methods the caller may use, as a GET's Allow
// does: the operations level and the write grant close the rest.
func TestOptionsAllowFollowsLevelAndGrant(t *testing.T) {
	spec.Satisfies(t, "http/rfc9110/405-sends-allow")

	readOnlyGrant := acl.NewEvaluator()
	readOnlyGrant.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources: []string{"*"}, Audience: []string{"*"}, Permissions: []string{"read"},
	}}})

	for _, tc := range []struct {
		name    string
		opts    []testHandlersOption
		options string
	}{
		{"level 0", []testHandlersOption{withOpsLevel(config.OpsReadOnly)}, "GET, HEAD, OPTIONS"},
		{"level 3", []testHandlersOption{withOpsLevel(config.OpsImpactful)}, "GET, HEAD, DELETE, OPTIONS"},
		{"no write grant", []testHandlersOption{
			withOpsLevel(config.OpsImpactful), withACL(readOnlyGrant),
		}, "GET, HEAD, OPTIONS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := newTestRouterWithCache(t, webService(), tc.opts...)

			req := httptest.NewRequest(http.MethodGet, "/services/svc1", nil)
			req = req.WithContext(
				auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "alice"}),
			)
			get := httptest.NewRecorder()
			router.ServeHTTP(get, req)

			options := httptest.NewRecorder()
			router.ServeHTTP(
				options,
				httptest.NewRequest(http.MethodOptions, "/services/svc1", nil),
			)

			if got := options.Header().Get("Allow"); got != tc.options {
				t.Errorf("OPTIONS Allow = %q, want %q", got, tc.options)
			}
			if want := strings.TrimSuffix(
				tc.options,
				", OPTIONS",
			); get.Header().
				Get("Allow") !=
				want {
				t.Errorf("GET Allow = %q, want %q", get.Header().Get("Allow"), want)
			}
			if !slices.Contains(options.Header().Values("Vary"), varyIdentity) {
				t.Errorf(
					"OPTIONS Vary = %q, want it to name %s",
					options.Header().Values("Vary"),
					varyIdentity,
				)
			}
		})
	}

	router := newTestRouterWithCache(t, webService(), withOpsLevel(config.OpsReadOnly))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/services/svc1", nil))
	if rec.Code != http.StatusMethodNotAllowed ||
		rec.Header().Get("Allow") != "GET, HEAD, OPTIONS" {
		t.Errorf("POST at level 0 = %d with Allow %q, want 405 with GET, HEAD, OPTIONS",
			rec.Code, rec.Header().Get("Allow"))
	}
}

// A manager clock ahead of this one stamps an UpdatedAt in the future, which a
// GET clamps; the date a client echoes back must still hold.
func TestIfUnmodifiedSinceUsesTheClampedDate(t *testing.T) {
	spec.Satisfies(t, "http/rfc9110/last-modified-is-not-in-the-future")

	c := webService()
	svc, _ := c.GetService("svc1")
	svc.UpdatedAt = time.Now().Add(time.Hour)
	c.SetService(svc)

	removed := 0
	router := newTestRouterWithCache(t, c, withWriteClient(&mockWriteClient{
		removeServiceFn: func(context.Context, string) error { removed++; return nil },
	}))

	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/services/svc1", nil))
	lastModified := get.Header().Get("Last-Modified")

	req := httptest.NewRequest(http.MethodDelete, "/services/svc1", nil)
	req.Header.Set("If-Unmodified-Since", lastModified)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent || removed != 1 {
		t.Errorf(
			"DELETE with the echoed Last-Modified = %d, want 204: %s",
			rec.Code,
			rec.Body.String(),
		)
	}
}

// The Accept check runs after the authorization gates, and a client asking for
// problem details alone may write.
func TestAcceptCheckFollowsAuthorization(t *testing.T) {
	scaled := 0
	c := webService()
	wc := &mockWriteClient{
		scaleServiceFn: func(_ context.Context, id string, _ uint64) (swarm.Service, error) {
			scaled++
			svc, _ := c.GetService(id)
			return svc, nil
		},
	}

	scale := func(router http.Handler, accept string) int {
		req := httptest.NewRequest(
			http.MethodPut,
			"/services/svc1/scale",
			strings.NewReader(`{"replicas":2}`),
		)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", accept)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		return rec.Code
	}

	readOnly := newTestRouterWithCache(t, c, withWriteClient(wc), withOpsLevel(config.OpsReadOnly))
	if got := scale(readOnly, "text/csv"); got != http.StatusForbidden {
		t.Errorf("a forbidden write asking for CSV = %d, want 403", got)
	}

	open := newTestRouterWithCache(t, c, withWriteClient(wc), withOpsLevel(config.OpsImpactful))
	if got := scale(open, "application/problem+json"); got != http.StatusOK || scaled != 1 {
		t.Errorf(
			"Accept: application/problem+json = %d after %d scales, want 200 after 1",
			got,
			scaled,
		)
	}
}

// return=minimal lifts the Accept check only where the answer is then bodiless.
func TestReturnMinimalLiftsTheAcceptCheckOnlyWithoutABody(t *testing.T) {
	installed, scaled := 0, 0
	c := webService()
	router := newTestRouterWithCache(t, c,
		withWriteClient(&mockWriteClient{
			scaleServiceFn: func(_ context.Context, id string, _ uint64) (swarm.Service, error) {
				scaled++
				svc, _ := c.GetService(id)
				return svc, nil
			},
		}),
		withPluginClient(&mockPluginClient{
			pluginInstallFn: func(context.Context, string) (*types.Plugin, error) {
				installed++
				return &types.Plugin{Name: "p:latest"}, nil
			},
		}),
	)

	send := func(method, target, body, prefer string) int {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/csv")
		req.Header.Set("Prefer", prefer)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		return rec.Code
	}

	if got := send(
		http.MethodPost,
		"/plugins",
		`{"remote":"p:latest","privileges":[]}`,
		"return=minimal",
	); got != http.StatusNotAcceptable ||
		installed != 0 {
		t.Errorf(
			"POST /plugins with return=minimal = %d after %d installs, want 406 and none",
			got,
			installed,
		)
	}
	for _, prefer := range []string{"return=minimal, respond-async", "return=minimal, wait=5"} {
		if got := send(
			http.MethodPut,
			"/services/svc1/scale",
			`{"replicas":2}`,
			prefer,
		); got != http.StatusNotAcceptable ||
			scaled != 0 {
			t.Errorf("scale with %q = %d after %d scales, want 406 and none", prefer, got, scaled)
		}
	}
}

// RFC 9110 §13.2.2 evaluates the preconditions before the Range.
func TestAFalseIfMatchOutranksAnUnsatisfiableRange(t *testing.T) {
	spec.Satisfies(t, "http/rfc9110/preconditions-are-evaluated-in-order")

	router := newTestRouterWithCache(t, webService())
	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	req.Header.Set("Range", "items=50-59")
	req.Header.Set("If-Match", `"stale"`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusPreconditionFailed {
		t.Errorf("status = %d, want 412", rec.Code)
	}
}

// HEAD answers what a GET would, a full cap included, without taking a slot.
func TestHeadOnAFullStreamAnswers429(t *testing.T) {
	assertFull := func(t *testing.T, rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
			t.Errorf("HEAD at the cap = %d, Retry-After %q; want 429 with one",
				rec.Code, rec.Header().Get("Retry-After"))
		}
	}

	t.Run("log tail", func(t *testing.T) {
		c := cache.New(nil)
		c.SetService(swarm.Service{ID: "svc1"})
		h := newTestHandlers(t, withCache(c), withDockerClient(&mockLogStreamer{}))
		for range maxLogSSEConns / 4 {
			_, _, ok := h.logStreams.Acquire(
				httptest.NewRequest(http.MethodGet, "/", nil),
				maxLogSSEConns,
			)
			if !ok {
				t.Fatal("could not fill the caller's share")
			}
		}

		req := withContentType(
			httptest.NewRequest(http.MethodHead, "/services/svc1/logs", nil),
			ContentTypeSSE,
		)
		req.SetPathValue("id", "svc1")
		rec := httptest.NewRecorder()
		before := h.logStreams.InUse()
		h.HandleServiceLogs(rec, req)

		assertFull(t, rec)
		if h.logStreams.InUse() != before {
			t.Errorf("HEAD took a slot")
		}
	})

	t.Run("metrics stream", func(t *testing.T) {
		h := newTestHandlers(t)
		h.promClient = promapi.NewClient("http://127.0.0.1:1")
		h.metricsStreamCount.Store(maxMetricsStreamClients)
		rec := httptest.NewRecorder()
		h.HandleMetricsStream(rec, httptest.NewRequest(http.MethodHead, "/metrics?query=up", nil))

		assertFull(t, rec)
		if h.metricsStreamCount.Load() != maxMetricsStreamClients {
			t.Errorf("HEAD changed the stream count")
		}
	})

}
