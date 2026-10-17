package acl

import (
	"fmt"
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
)

// The resolver scans the cache under its lock, so how often a filter asks it
// is a cost that grows with the grant count unless the matcher bounds it.
func TestFilterResolvesEachItemOnceAtMost(t *testing.T) {
	names := make([]string, 50)
	for i := range names {
		names[i] = fmt.Sprintf("svc-%d", i)
	}

	grant := func(resource string) Grant {
		return Grant{
			Resources:   []string{resource},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		}
	}
	id := &auth.Identity{Subject: "anyone"}

	tests := []struct {
		name       string
		grants     []Grant
		resType    string
		wantStacks int
		wantTasks  int
	}{
		{
			name:    "no grant names a stack",
			grants:  []Grant{grant("service:a"), grant("service:b"), grant("config:c")},
			resType: "service",
		},
		{
			name:       "several grants name stacks",
			grants:     []Grant{grant("stack:a"), grant("stack:b"), grant("stack:c")},
			resType:    "service",
			wantStacks: len(names),
		},
		{
			name:       "a type wildcard can name a stack",
			grants:     []Grant{grant("*:a"), grant("*:b")},
			resType:    "service",
			wantStacks: len(names),
		},
		{
			name:    "tasks under grants naming neither services nor stacks",
			grants:  []Grant{grant("config:a"), grant("secret:b")},
			resType: "task",
		},
		{
			name:       "tasks under service and stack grants",
			grants:     []Grant{grant("service:a"), grant("service:b"), grant("stack:c")},
			resType:    "task",
			wantStacks: len(names),
			wantTasks:  len(names),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &stubResolver{}
			e := NewEvaluator()
			e.SetResolver(resolver)
			e.SetPolicy(&Policy{Grants: tt.grants})

			Filter(e, id, "read", names, func(n string) string { return tt.resType + ":" + n })

			if resolver.stackLookups > tt.wantStacks || resolver.serviceLookups > tt.wantTasks {
				t.Errorf(
					"Filter: %d stack and %d task lookups, want at most %d and %d",
					resolver.stackLookups, resolver.serviceLookups, tt.wantStacks, tt.wantTasks,
				)
			}

			resolver.stackLookups, resolver.serviceLookups = 0, 0
			FilterInPlaceNamed(e, id, "read", append([]string(nil), names...), tt.resType,
				func(n string) string { return n })

			if resolver.stackLookups > tt.wantStacks || resolver.serviceLookups > tt.wantTasks {
				t.Errorf(
					"FilterInPlaceNamed: %d stack and %d task lookups, want at most %d and %d",
					resolver.stackLookups, resolver.serviceLookups, tt.wantStacks, tt.wantTasks,
				)
			}
		})
	}
}

type countingSource struct {
	grants []Grant
	calls  int
}

func (s *countingSource) GrantsFor(*auth.Identity) []Grant {
	s.calls++
	return s.grants
}

// A source parses a header or claim on every call, and the SSE filter asks Can
// for every event a subscriber is sent, so one identity derives its grants once.
func TestSourceGrantsAreDerivedOncePerIdentity(t *testing.T) {
	source := &countingSource{grants: []Grant{
		{Resources: []string{"service:web"}, Permissions: []string{"read"}},
	}}
	e := NewEvaluator()
	e.SetPolicy(&Policy{})
	e.SetSource(source)

	id := &auth.Identity{Subject: "alice"}
	for range 3 {
		if !e.Can(id, "read", "service:web") {
			t.Fatal("the source's grant did not apply")
		}
	}
	Filter(e, id, "read", []string{"service:web"}, func(s string) string { return s })
	e.Fingerprint(id)

	if source.calls != 1 {
		t.Errorf("GrantsFor called %d times for one identity, want 1", source.calls)
	}

	if !e.Can(&auth.Identity{Subject: "bob"}, "read", "service:web") || source.calls != 2 {
		t.Errorf("a second identity did not derive its own grants: %d calls", source.calls)
	}

	// Another evaluator's source answers for the same identity on its own.
	other := &countingSource{}
	e2 := NewEvaluator()
	e2.SetPolicy(&Policy{})
	e2.SetSource(other)
	if e2.Can(id, "read", "service:web") || other.calls != 1 {
		t.Errorf("a second source reused the first one's grants: %d calls", other.calls)
	}
}
