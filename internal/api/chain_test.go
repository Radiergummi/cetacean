package api

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// recorder returns a Constructor that appends name to order when it runs.
func recorder(order *[]string, name string) Constructor {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*order = append(*order, name)
			next.ServeHTTP(w, r)
		})
	}
}

func run(t *testing.T, h http.Handler) {
	t.Helper()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}

func TestChainRunsInDeclaredOrder(t *testing.T) {
	var order []string
	h := NewChain(
		recorder(&order, "first"),
		recorder(&order, "second"),
		recorder(&order, "third"),
	).ThenFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
	})

	run(t, h)

	want := []string{"first", "second", "third", "handler"}
	if !slices.Equal(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}
}

// TestDerivedChainsDoNotContaminate is why Chain copies rather than appending in
// place. Reproducing the corruption needs a base at len == cap, one Append to
// produce a middle chain with spare capacity, and two derivations from that
// middle chain: a naive append writes the same index for both.
func TestDerivedChainsDoNotContaminate(t *testing.T) {
	var order []string
	base := NewChain(
		recorder(&order, "m1"),
		recorder(&order, "m2"),
	)

	middle := base.Append(recorder(&order, "m3"))
	withA := middle.Append(recorder(&order, "markerA"))
	withB := middle.Append(recorder(&order, "markerB"))

	if &withA.constructors[3] == &withB.constructors[3] {
		t.Fatal("derived chains share a backing array")
	}

	order = nil
	run(t, withA.ThenFunc(func(http.ResponseWriter, *http.Request) {}))
	if want := []string{"m1", "m2", "m3", "markerA"}; !slices.Equal(order, want) {
		t.Errorf("withA order = %v, want %v", order, want)
	}

	order = nil
	run(t, withB.ThenFunc(func(http.ResponseWriter, *http.Request) {}))
	if want := []string{"m1", "m2", "m3", "markerB"}; !slices.Equal(order, want) {
		t.Errorf("withB order = %v, want %v", order, want)
	}
}

func TestChainNewCopiesCallerSlice(t *testing.T) {
	var order []string
	given := []Constructor{recorder(&order, "original")}
	c := NewChain(given...)

	given[0] = recorder(&order, "mutated")

	run(t, c.ThenFunc(func(http.ResponseWriter, *http.Request) {}))
	if want := []string{"original"}; !slices.Equal(order, want) {
		t.Errorf("order = %v, want %v — NewChain did not copy", order, want)
	}
}

func TestChainExtendComposes(t *testing.T) {
	var order []string
	outer := NewChain(recorder(&order, "outer"))
	inner := NewChain(recorder(&order, "inner"))

	run(t, outer.Extend(inner).ThenFunc(func(http.ResponseWriter, *http.Request) {}))
	if want := []string{"outer", "inner"}; !slices.Equal(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}
}

func TestChainThenPanicsOnNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Then(nil) did not panic")
		}
	}()
	NewChain().Then(nil)
}
