package recommendations

import (
	"slices"
	"testing"
)

func TestACLResource(t *testing.T) {
	for scope, want := range map[Scope]string{
		ScopeService: "service:web",
		ScopeNode:    "node:web",
		ScopeNetwork: "network:web",
		ScopeCluster: "swarm:cluster",
	} {
		rec := Recommendation{Scope: scope, TargetName: "web"}
		if got := rec.ACLResource(); got != want {
			t.Errorf("%s: got %q, want %q", scope, got, want)
		}
	}
}

// A recommendation is hidden for a resource it reveals as well as its target.
func TestReadable(t *testing.T) {
	recs := []Recommendation{
		{Scope: ScopeService, TargetName: "web"},
		{Scope: ScopeService, TargetName: "db"},
		{Scope: ScopeService, TargetName: "web", Reveals: []string{"node:secret-host"}},
	}
	canRead := func(resource string) bool { return resource == "service:web" }

	got := Readable(recs, canRead)

	if len(got) != 1 || got[0].TargetName != "web" || len(got[0].Reveals) != 0 {
		t.Errorf("Readable() = %+v, want only the web recommendation revealing nothing", got)
	}

	if slices.ContainsFunc(got, func(r Recommendation) bool { return r.TargetName == "db" }) {
		t.Error("Readable() kept a recommendation on an unreadable target")
	}
}
