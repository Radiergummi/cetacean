package sse

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// One caller fills only its share; another still gets a slot, and a released
// slot is the caller's to take again.
func TestSlotsHoldEachCallerToAShare(t *testing.T) {
	var slots Slots

	from := func(addr string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = addr

		return r
	}

	var releases []func()
	for range 2 {
		release, _, ok := slots.Acquire(from("10.0.0.1:1"), 8)
		if !ok {
			t.Fatal("a caller was refused inside its share")
		}
		releases = append(releases, release)
	}

	if _, callerFull, ok := slots.Acquire(from("10.0.0.1:2"), 8); ok || !callerFull {
		t.Fatalf(
			"past its share: ok=%v callerFull=%v, want refused as the caller's",
			ok,
			callerFull,
		)
	}
	if _, _, ok := slots.Acquire(from("10.0.0.2:1"), 8); !ok {
		t.Fatal("another caller was refused while slots were free")
	}

	releases[0]()
	if _, _, ok := slots.Acquire(from("10.0.0.1:3"), 8); !ok {
		t.Error("a released slot was not the caller's to take again")
	}
}
