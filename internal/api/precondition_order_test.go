package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/spec"
)

var (
	serviceUpdated = time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	beforeUpdate   = serviceUpdated.Add(-24 * time.Hour).Format(http.TimeFormat)
	afterUpdate    = serviceUpdated.Add(24 * time.Hour).Format(http.TimeFormat)
)

// datedServiceRouter serves one service last updated at serviceUpdated, and
// counts what reached the engine.
func datedServiceRouter(t *testing.T) (http.Handler, *int, *int) {
	t.Helper()

	replicas := uint64(1)
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Meta: swarm.Meta{UpdatedAt: serviceUpdated},
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Env: []string{"A=1"}},
			},
		},
	})

	var removed, written int
	router := newTestRouterWithCache(t, c, withWriteClient(&mockWriteClient{
		removeServiceFn: func(context.Context, string) error {
			removed++
			return nil
		},
		scaleServiceFn: func(_ context.Context, id string, _ uint64) (swarm.Service, error) {
			written++
			svc, _ := c.GetService(id)
			return svc, nil
		},
		updateServiceEnvFn: func(_ context.Context, id string, _ map[string]string) (swarm.Service, error) {
			written++
			svc, _ := c.GetService(id)
			return svc, nil
		},
	}))

	return router, &removed, &written
}

func serve(router http.Handler, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Accept", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	return rec
}

// RFC 9110 §13.2.2: If-Match, else If-Unmodified-Since, then If-None-Match,
// and a false one stops a write with 412.
func TestWritePreconditionsAreEvaluatedInOrder(t *testing.T) {
	spec.Satisfies(t,
		"http/rfc9110/preconditions-are-evaluated-in-order",
		"http/rfc9110/if-none-match-is-evaluated-before-the-method",
		"http/rfc9110/a-false-if-none-match-answers-304",
		"http/rfc9110/if-unmodified-since-is-evaluated",
		"http/rfc9110/if-unmodified-since-yields-to-if-match",
		"http/rfc9110/a-malformed-if-unmodified-since-is-ignored",
		"http/rfc9110/a-false-if-unmodified-since-stops-the-method",
		"http/rfc9110/a-failed-if-unmodified-since-may-answer-412",
	)

	router, removed, _ := datedServiceRouter(t)
	etag := serve(router, http.MethodGet, "/services/svc1", "", nil).Header().Get("ETag")
	if etag == "" {
		t.Fatal("GET /services/svc1 sent no ETag")
	}

	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"If-None-Match: * on a resource that exists", map[string]string{"If-None-Match": "*"}, 412},
		{"If-None-Match: the current tag", map[string]string{"If-None-Match": etag}, 412},
		{"If-None-Match: a stale tag", map[string]string{"If-None-Match": `"stale"`}, 204},
		{"If-Unmodified-Since before the update", map[string]string{"If-Unmodified-Since": beforeUpdate}, 412},
		{"If-Unmodified-Since after the update", map[string]string{"If-Unmodified-Since": afterUpdate}, 204},
		{"a malformed If-Unmodified-Since", map[string]string{"If-Unmodified-Since": "last week"}, 204},
		{"a list of dates", map[string]string{"If-Unmodified-Since": beforeUpdate + ", " + afterUpdate}, 204},
		{"If-Match outranks If-Unmodified-Since", map[string]string{
			"If-Match": etag, "If-Unmodified-Since": beforeUpdate,
		}, 204},
		{"If-None-Match is evaluated after a true If-Match", map[string]string{
			"If-Match": etag, "If-None-Match": etag,
		}, 412},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := *removed
			rec := serve(router, http.MethodDelete, "/services/svc1", "", tc.headers)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			if ran := *removed - before; ran != map[bool]int{true: 1, false: 0}[tc.want == 204] {
				t.Errorf("the removal ran %d times", ran)
			}
		})
	}
}

// A resource with no modification date ignores If-Unmodified-Since, and one
// with no representation at all has none for If-None-Match to find.
func TestWritePreconditionsWithoutAValidator(t *testing.T) {
	spec.Satisfies(t, "http/rfc9110/if-unmodified-since-needs-a-modification-date")

	router, _, written := datedServiceRouter(t)

	rec := serve(router, http.MethodPatch, "/services/svc1/env", `{"B":"2"}`, map[string]string{
		"Content-Type":        "application/merge-patch+json",
		"If-Unmodified-Since": beforeUpdate,
	})
	if rec.Code != http.StatusOK || *written != 1 {
		t.Errorf("PATCH env under If-Unmodified-Since = %d after %d writes, want 200 after 1: %s",
			rec.Code, *written, rec.Body.String())
	}

	rec = serve(router, http.MethodPut, "/services/svc1/scale", `{"replicas":2}`, map[string]string{
		"Content-Type":  "application/json",
		"If-None-Match": "*",
	})
	if rec.Code != http.StatusOK || *written != 2 {
		t.Errorf("PUT scale under If-None-Match: * = %d after %d writes, want 200 after 2",
			rec.Code, *written)
	}
}

// If-Match and If-Unmodified-Since bind a GET too, ahead of the cache
// validators: a false one is 412, not the representation and not a 304.
func TestReadPreconditionsAreEvaluatedInOrder(t *testing.T) {
	spec.Satisfies(t,
		"http/rfc9110/if-match-is-evaluated-before-the-method",
		"http/rfc9110/a-false-if-match-stops-the-method",
		"http/rfc9110/preconditions-are-evaluated-in-order",
	)

	router, _, _ := datedServiceRouter(t)

	for _, target := range []string{"/services/svc1", "/services"} {
		etag := serve(router, http.MethodGet, target, "", nil).Header().Get("ETag")

		for _, tc := range []struct {
			name    string
			headers map[string]string
			want    int
		}{
			{"a stale If-Match", map[string]string{"If-Match": `"stale"`}, 412},
			{"the current If-Match", map[string]string{"If-Match": etag}, 200},
			{"a true If-Match, then If-None-Match", map[string]string{
				"If-Match": etag, "If-None-Match": etag,
			}, 304},
			{"a false If-Match before a matching If-None-Match", map[string]string{
				"If-Match": `"stale"`, "If-None-Match": etag,
			}, 412},
		} {
			if rec := serve(router, http.MethodGet, target, "", tc.headers); rec.Code != tc.want {
				t.Errorf("GET %s with %s = %d, want %d", target, tc.name, rec.Code, tc.want)
			}
		}
	}

	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"If-Unmodified-Since before the update", map[string]string{"If-Unmodified-Since": beforeUpdate}, 412},
		{"If-Unmodified-Since after the update", map[string]string{"If-Unmodified-Since": afterUpdate}, 200},
	} {
		if rec := serve(router, http.MethodGet, "/services/svc1", "", tc.headers); rec.Code != tc.want {
			t.Errorf("GET with %s = %d, want %d", tc.name, rec.Code, tc.want)
		}
	}
}
