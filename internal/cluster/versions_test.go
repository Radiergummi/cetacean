package cluster

import (
	"encoding/json"
	"maps"
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
	if v := groups.Version("api"); v != (Version{Newest: true}) {
		t.Errorf("Version(api) = %+v, want its own newest version", v)
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

	// A previous spec names what a rollback restores, so it counts as a use.
	c.SetService(swarm.Service{
		ID:   "svc",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "api"}},
		PreviousSpec: &swarm.ServiceSpec{TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Secrets: []*swarm.SecretReference{{SecretID: "db_v1"}},
			},
		}},
	})
	if users := c.ServicesUsingSecret("db_v1"); len(users) != 1 || !users[0].Rollback {
		t.Errorf(
			"ServicesUsingSecret(db_v1) = %v, want the service whose previous spec names it",
			users,
		)
	}

	event := cache.Event{Type: cache.EventSecret, Resource: versionedSecret("db_v4", 9)}
	_, touched := index.DigestsTouchedBy(event)
	if len(touched) != 3 {
		t.Errorf("DigestsTouchedBy(db_v4) = %v, want the three other db versions", touched)
	}
}

// A version relabelled into another group leaves its old one, whose members
// must hear of it as well as those of the group it joins.
// The REST detail marshals VersionDetail and MCP's digest copies Map; both
// must carry the same keys, an ungrouped item's newestVersion included.
func TestVersionDetailMapMatchesItsJSON(t *testing.T) {
	for _, v := range []Version{
		{Newest: true},
		{Of: "db", Inferred: true, Newest: true, OlderVersions: 2},
		{Of: "db"},
	} {
		body, err := json.Marshal(v.Detail(1))
		if err != nil {
			t.Fatal(err)
		}

		var fromJSON map[string]any
		if err := json.Unmarshal(body, &fromJSON); err != nil {
			t.Fatal(err)
		}

		if got, want := slices.Sorted(
			maps.Keys(v.Detail(1).Map()),
		), slices.Sorted(
			maps.Keys(fromJSON),
		); !slices.Equal(
			got,
			want,
		) {
			t.Errorf("%+v: Map keys %v, JSON keys %v", v, got, want)
		}
	}
}

// Every stream asks about one event in turn; the answer is worked out for the
// first and handed to the rest, even once the cache has moved on.
func TestVersionIndexAnswersEachEventOnce(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(versionedSecret("db_v1", 1))

	var event cache.Event
	c.AddOnChangeListener(func(e cache.Event) { event = e })
	c.SetSecret(versionedSecret("db_v2", 2))

	created := event
	index := NewVersionIndex(c)
	_, first := index.DigestsTouchedBy(created)

	c.SetSecret(versionedSecret("db_v3", 3))
	_, again := index.DigestsTouchedBy(created)

	if len(first) != 1 || len(again) != 1 {
		t.Errorf("touched %v, then %v for the same event, want db_v1 both times", first, again)
	}
}

func TestVersionIndexFollowsARelabel(t *testing.T) {
	c := cache.New(nil)
	for i, name := range []string{"db_v1", "db_v2", "other_v1"} {
		c.SetSecret(versionedSecret(name, i+1))
	}

	index := NewVersionIndex(c)

	// The event the cache sends carries the record it replaced.
	var event cache.Event
	c.AddOnChangeListener(func(e cache.Event) { event = e })

	moved := versionedSecret("db_v2", 2)
	moved.Spec.Labels = map[string]string{"cetacean.version-of": "other"}
	c.SetSecret(moved)

	_, touched := index.DigestsTouchedBy(event)

	var ids []string
	for _, ref := range touched {
		ids = append(ids, ref.ID)
	}
	slices.Sort(ids)

	if !slices.Equal(ids, []string{"db_v1", "other_v1"}) {
		t.Errorf("DigestsTouchedBy(relabel) = %v, want the group left and the group joined", ids)
	}
}
