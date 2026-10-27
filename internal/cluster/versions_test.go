package cluster

import (
	"slices"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

func versionedSecret(name string, day int) swarm.Secret {
	return swarm.Secret{
		ID:        name,
		CreatedAt: time.Date(2026, 1, day, 0, 0, 0, 0, time.UTC),
		Spec:      swarm.SecretSpec{Annotations: swarm.Annotations{Name: name}},
	}
}

func TestSelectVersions(t *testing.T) {
	secrets := []swarm.Secret{
		versionedSecret("db_v1", 1),
		versionedSecret("api", 1),
		versionedSecret("db_v3", 3),
		versionedSecret("db_v2", 2),
	}
	groups := SecretVersionGroups(secrets)
	id := func(s swarm.Secret) string { return s.ID }

	tests := []struct {
		name string
		sel  VersionSelection
		want []string
	}{
		{"newest", VersionSelection{}, []string{"api", "db_v3"}},
		{"all", VersionSelection{All: true}, []string{"db_v1", "api", "db_v3", "db_v2"}},
		{
			"one group, newest first",
			VersionSelection{Of: "db_v1"},
			[]string{"db_v3", "db_v2", "db_v1"},
		},
		{"an ungrouped item is its own group", VersionSelection{Of: "api"}, []string{"api"}},
		{"an unknown ID names nothing", VersionSelection{Of: "gone"}, nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got []string
			for _, s := range SelectVersions(secrets, id, groups, test.sel) {
				got = append(got, s.ID)
			}

			if !slices.Equal(got, test.want) {
				t.Errorf("SelectVersions() = %v, want %v", got, test.want)
			}
		})
	}

	if v := groups.Version("db_v3"); !v.Newest || v.OlderVersions != 2 || v.Of != "db" {
		t.Errorf("Version(db_v3) = %+v", v)
	}
	if v := groups.Version("api"); v != (Version{}) {
		t.Errorf("Version(api) = %+v, want the zero Version", v)
	}
}

func TestParseVersionSelection(t *testing.T) {
	tests := []struct {
		value     string
		searching bool
		all       bool
	}{
		{"", false, false},
		{"newest", false, false},
		{"all", false, true},
		// A search finds an older version by name unless asked not to.
		{"", true, true},
		{"newest", true, false},
	}

	for _, test := range tests {
		sel, err := ParseVersionSelection(test.value, "", test.searching)
		if err != nil || sel.All != test.all {
			t.Errorf(
				"ParseVersionSelection(%q, %v) = %+v, %v",
				test.value,
				test.searching,
				sel,
				err,
			)
		}
	}

	if _, err := ParseVersionSelection("latest", "", false); err == nil {
		t.Error("ParseVersionSelection(latest) accepted an unknown value")
	}
}

// One detail groups only the readable versions sharing the item's key, and the
// index follows the cache as it changes.
func TestVersionIndex(t *testing.T) {
	c := cache.New(nil)
	for i, name := range []string{"db_v1", "db_v2", "db_v3", "other_v1", "other_v2"} {
		c.SetSecret(versionedSecret(name, i+1))
	}

	index := NewVersionIndex(c)
	canRead := func(resource string) bool { return resource != "secret:db_v3" }

	if v := index.SecretVersion(versionedSecret("db_v2", 2), canRead); !v.Newest ||
		v.OlderVersions != 1 || v.Of != "db" {
		t.Errorf("SecretVersion(db_v2) = %+v, want the newest readable of two", v)
	}

	c.SetSecret(versionedSecret("db_v4", 9))

	if v := index.SecretVersion(versionedSecret("db_v2", 2), canRead); v.Newest {
		t.Errorf("SecretVersion(db_v2) = %+v after db_v4 arrived, want an older version", v)
	}

	event := cache.Event{Type: cache.EventSecret, Resource: versionedSecret("db_v4", 9)}
	_, touched := index.DigestsTouchedBy(event)
	if len(touched) != 3 {
		t.Errorf("DigestsTouchedBy(db_v4) = %v, want the three other db versions", touched)
	}
}
