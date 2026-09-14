package acl

import (
	"fmt"
	"slices"
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
)

func readGrant(resources ...string) Grant {
	return Grant{Resources: resources, Permissions: []string{"read"}}
}

// The property everything keyed on a fingerprint depends on: two identities that
// may see different things never share one. A collision here serves one user's
// rows to another.
func TestFingerprintSeparatesIdentities(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"service:*"},
			Permissions: []string{"read"},
			Audience:    []string{"group:admins"},
		},
		{
			Resources:   []string{"service:web"},
			Permissions: []string{"read"},
			Audience:    []string{"group:devs"},
		},
	}})

	admin := &auth.Identity{Subject: "a", Groups: []string{"admins"}}
	dev := &auth.Identity{Subject: "d", Groups: []string{"devs"}}
	nobody := &auth.Identity{Subject: "n"}

	fa, fd, fn := e.Fingerprint(admin), e.Fingerprint(dev), e.Fingerprint(nobody)

	if fa == fd {
		t.Error("an admin and a developer share a fingerprint")
	}
	if fa == fn || fd == fn {
		t.Error("an identity with no grants shares a fingerprint with one that has them")
	}
}

func TestFingerprintStableForSameGrants(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"service:*"},
			Permissions: []string{"read"},
			Audience:    []string{"group:devs"},
		},
	}})

	one := &auth.Identity{Subject: "one", Groups: []string{"devs"}}
	two := &auth.Identity{Subject: "two", Groups: []string{"devs"}}

	if e.Fingerprint(one) != e.Fingerprint(two) {
		t.Error("two identities with identical grants do not share a fingerprint, " +
			"so nothing keyed on it can ever be reused between them")
	}
	first := e.Fingerprint(one)
	second := e.Fingerprint(one)
	if first != second {
		t.Errorf("fingerprint is not stable across calls: %d then %d", first, second)
	}
}

// Grant order is a property of the policy file, not of what an identity may see.
func TestFingerprintIgnoresGrantOrder(t *testing.T) {
	id := &auth.Identity{Subject: "x"}

	a := NewEvaluator()
	a.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:b", "service:a"}, Permissions: []string{"write", "read"}},
		readGrant("node:*"),
	}})

	b := NewEvaluator()
	b.SetPolicy(&Policy{Grants: []Grant{
		readGrant("node:*"),
		{Resources: []string{"service:a", "service:b"}, Permissions: []string{"read", "write"}},
	}})

	if a.Fingerprint(id) != b.Fingerprint(id) {
		t.Error("reordering a policy changed the fingerprint, so an unrelated edit " +
			"needlessly invalidates every cached response")
	}
}

// A reload changes what an identity may see with no cache mutation behind it.
func TestFingerprintChangesOnPolicyReload(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{readGrant("service:*")}})

	id := &auth.Identity{Subject: "x"}
	before := e.Fingerprint(id)

	e.SetPolicy(&Policy{Grants: []Grant{readGrant("service:web")}})

	if e.Fingerprint(id) == before {
		t.Error("narrowing the policy left the fingerprint unchanged; a client would " +
			"keep being told its copy of the wider listing is current")
	}
}

func TestFingerprintDistinguishesResourcesFromPermissions(t *testing.T) {
	id := &auth.Identity{Subject: "x"}

	a := NewEvaluator()
	a.SetPolicy(&Policy{Grants: []Grant{{
		Resources: []string{"service:a"}, Permissions: []string{"read"},
	}}})

	b := NewEvaluator()
	b.SetPolicy(&Policy{Grants: []Grant{{
		Resources: []string{"service:a", "read"}, Permissions: []string{},
	}}})

	if a.Fingerprint(id) == b.Fingerprint(id) {
		t.Error("the same characters split differently between resources and " +
			"permissions hash alike")
	}
}

func TestFingerprintNilAndUnconfigured(t *testing.T) {
	var nilEval *Evaluator
	if nilEval.Fingerprint(nil) != 0 {
		t.Error("a nil evaluator should fingerprint as the one unfiltered answer")
	}
	if NewEvaluator().Fingerprint(nil) != 0 {
		t.Error("an evaluator with no policy filters nothing and should fingerprint as 0")
	}
}

// FilterInPlace must keep exactly what Filter keeps, in the same order — it is
// only allowed to differ in what it does to the caller's backing array.
func TestFilterInPlaceMatchesFilter(t *testing.T) {
	policies := map[string][]Grant{
		"wildcard":   {readGrant("service:*")},
		"prefix":     {readGrant("service:web*")},
		"none":       {readGrant("node:*")},
		"two grants": {readGrant("service:web-1"), readGrant("service:web-3")},
	}

	names := make([]string, 0, 12)
	for i := range 12 {
		names = append(names, fmt.Sprintf("web-%d", i))
	}
	resource := func(s string) string { return "service:" + s }

	for name, grants := range policies {
		t.Run(name, func(t *testing.T) {
			e := NewEvaluator()
			e.SetPolicy(&Policy{Grants: grants})

			copied := slices.Clone(names)
			inPlace := slices.Clone(names)

			want := Filter(e, nil, "read", copied, resource)
			got := FilterInPlace(e, nil, "read", inPlace, resource)

			if !slices.Equal(want, got) {
				t.Errorf("FilterInPlace gave %v, Filter gave %v", got, want)
			}
			if !slices.Equal(copied, names) {
				t.Error("Filter modified the slice it was given")
			}
		})
	}
}

func TestFilterInPlaceUnconfigured(t *testing.T) {
	items := []string{"a", "b"}
	if got := FilterInPlace(
		nil,
		nil,
		"read",
		items,
		func(s string) string { return s },
	); len(
		got,
	) != 2 {
		t.Errorf("a nil evaluator filtered %d of 2 items", len(got))
	}
}
