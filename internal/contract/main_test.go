package contract

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestMain runs the package's sweeps and then asks what they actually reached.
// The question can only be answered afterwards, which is why it lives here
// rather than in an ordinary test: a test asserting on the recorder would see
// only the routes exercised before it happened to run.
func TestMain(m *testing.M) {
	code := m.Run()

	// The gate asks which routes this package's sweeps reached, and that has an
	// answer only when every sweep ran. Under `go test -run TestX` the rest are
	// filtered out, so almost nothing is recorded and the gate would fail a run
	// that never exercised what it measures.
	if code == 0 && !runFiltered() {
		routes, err := Routes()
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nFAIL\tinternal/contract\tRoutes: %v\n", err)

			code = 1
		} else {
			reached := exercisedRoutes()

			if failures := uncoveredRoutes(routes, reached, excusedUncovered); len(failures) > 0 {
				fmt.Fprintf(os.Stderr,
					"\nFAIL\tinternal/contract\troutes exercised by no sweep and carrying "+
						"no excuse:\n  %s\n\nAdd a sweep that reaches them, or add them to "+
						"excusedUncovered in excused.go with a reason a reader can "+
						"evaluate. \"Not yet\" is not a reason — that is a coverage gap, "+
						"and it belongs on the defect list.\n",
					strings.Join(failures, "\n  "),
				)

				code = 1
			}

			if stale := staleExcuses(reached, excusedUncovered); len(stale) > 0 {
				fmt.Fprintf(os.Stderr,
					"\nFAIL\tinternal/contract\troutes excused in excusedUncovered that "+
						"a sweep now reaches:\n  %s\n\nRemove the excuses: a stale excuse "+
						"hides the next real gap.\n",
					strings.Join(stale, "\n  "),
				)

				code = 1
			}
		}
	}

	os.Exit(code)
}

// uncoveredRoutes returns the routes that no sweep reached and no excuse
// covers. It takes the inventory, the reached patterns and the excuse map as
// parameters — rather than reading them directly — so both directions of the
// gate can be proven on synthetic inputs without touching a product file.
func uncoveredRoutes(routes []Route, reached []string, excuses map[string]string) []string {
	var out []string

	for _, route := range routes {
		key := route.String()

		if slices.Contains(reached, key) {
			continue
		}

		if reason, excused := excuses[key]; excused {
			if strings.TrimSpace(reason) == "" {
				out = append(out, key+" (excused with an empty reason)")
			}

			continue
		}

		out = append(out, key)
	}

	return out
}

// staleExcuses returns excuses for routes a sweep now reaches, given the
// patterns reached so far and the excuse map. Parameterized for the same
// reason as uncoveredRoutes.
func staleExcuses(reached []string, excuses map[string]string) []string {
	var out []string

	for key := range excuses {
		if slices.Contains(reached, key) {
			out = append(out, key)
		}
	}

	slices.Sort(out)

	return out
}

func TestRecorderObservesRequests(t *testing.T) {
	w := NewWorld(t)

	resp := w.REST(t, "ops", "GET", "/services/"+SeededServiceID)
	resp.Body.Close()

	if !slices.Contains(exercisedRoutes(), "GET /services/{id}") {
		t.Errorf(
			"the recorder did not observe GET /services/{id}. Every coverage "+
				"claim this package makes rests on it, so a silent recorder "+
				"would make the whole self-enforcement vacuous.\nrecorded: %v",
			exercisedRoutes(),
		)
	}
}

// TestEventsReturnsImmediatelyForAPlainJSONRequest proves GET /events belongs
// on the "reached" list rather than excused as a stream that holds the
// connection open: only the ContentTypeSSE branch blocks, and an
// Accept: application/json request is refused 406 immediately. The 2s
// deadline fails loudly if the default branch ever falls through to SSE.
func TestEventsReturnsImmediatelyForAPlainJSONRequest(t *testing.T) {
	w := NewWorld(t)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.Server.URL+"/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "application/json")
	setPersonaHeaders(t, req, "ops")

	resp, err := w.Server.Client().Do(req)
	if err != nil {
		t.Fatalf(
			"GET /events with Accept: application/json did not return within "+
				"2s: %v. It should be refused as a type this route cannot "+
				"produce, rather than taken to the SSE branch, which holds the "+
				"connection open indefinitely.",
			err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotAcceptable {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /events = %d, want 406; body: %s", resp.StatusCode, body)
	}

	if !slices.Contains(exercisedRoutes(), "GET /events") {
		t.Errorf(
			"the recorder did not observe GET /events.\nrecorded: %v",
			exercisedRoutes(),
		)
	}
}

// TestUncoveredRoutesReportsAGenuineGap proves the first direction of the
// gate: a route neither reached by any sweep nor excused is reported.
func TestUncoveredRoutesReportsAGenuineGap(t *testing.T) {
	routes := []Route{{Method: "GET", Pattern: "/gap"}}

	failures := uncoveredRoutes(routes, nil, map[string]string{})

	if !slices.Contains(failures, "GET /gap") {
		t.Errorf("expected GET /gap to be reported uncovered, got %v", failures)
	}
}

// TestUncoveredRoutesIsSilentWhenReached proves a route a sweep reached, and
// that carries no excuse, is not reported — an excuse is not required for a
// route sweeps genuinely cover.
func TestUncoveredRoutesIsSilentWhenReached(t *testing.T) {
	routes := []Route{{Method: "GET", Pattern: "/reached"}}

	failures := uncoveredRoutes(routes, []string{"GET /reached"}, map[string]string{})

	if len(failures) != 0 {
		t.Errorf("expected no failures for a reached, unexcused route, got %v", failures)
	}
}

// TestUncoveredRoutesIsSilentWhenExcused proves a route no sweep reaches, but
// that carries a real excuse, is not reported.
func TestUncoveredRoutesIsSilentWhenExcused(t *testing.T) {
	routes := []Route{{Method: "GET", Pattern: "/excused"}}

	failures := uncoveredRoutes(routes, nil, map[string]string{
		"GET /excused": "genuinely excluded from in-process coverage, for the test",
	})

	if len(failures) != 0 {
		t.Errorf("expected no failures for an excused, unreached route, got %v", failures)
	}
}

// TestUncoveredRoutesReportsAnEmptyExcuse proves an excuse whose reason is
// blank is reported rather than silently accepted — an excuse with no reason
// a reader can evaluate is worse than no excuse at all.
func TestUncoveredRoutesReportsAnEmptyExcuse(t *testing.T) {
	routes := []Route{{Method: "GET", Pattern: "/blank"}}

	failures := uncoveredRoutes(routes, nil, map[string]string{"GET /blank": "   "})

	if !containsSubstring(failures, "GET /blank") {
		t.Errorf("expected GET /blank to be reported for its empty reason, got %v", failures)
	}
}

// TestStaleExcusesReportsAReachedRoute proves the second direction of the
// gate: an excuse for a route a sweep now reaches is reported, because a
// stale excuse hides the next real gap.
func TestStaleExcusesReportsAReachedRoute(t *testing.T) {
	excuses := map[string]string{"GET /now-reached": "used to need this excuse"}

	stale := staleExcuses([]string{"GET /now-reached"}, excuses)

	if !slices.Contains(stale, "GET /now-reached") {
		t.Errorf("expected GET /now-reached to be reported stale, got %v", stale)
	}
}

// TestStaleExcusesIsSilentWhenStillUnreached proves an excuse for a route no
// sweep reaches is not reported as stale.
func TestStaleExcusesIsSilentWhenStillUnreached(t *testing.T) {
	excuses := map[string]string{"GET /still-unreached": "genuinely excluded"}

	stale := staleExcuses(nil, excuses)

	if len(stale) != 0 {
		t.Errorf("expected no stale excuses for an unreached route, got %v", stale)
	}
}

// runFiltered reports whether -run narrowed this invocation to a subset of the
// package's tests.
func runFiltered() bool {
	f := flag.Lookup("test.run")
	if f == nil {
		return false
	}

	switch v := f.Value.String(); v {
	case "", ".*", "^.*$":
		return false
	default:
		return true
	}
}
