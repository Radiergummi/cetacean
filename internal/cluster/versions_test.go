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
	id := func(s swarm.Secret) string { return s.ID }
	unused := func(string) bool { return false }

	tests := []struct {
		name  string
		inUse func(string) bool
		sel   VersionSelection
		want  []string
	}{
		{"an unused older version is hidden", unused, VersionSelection{}, []string{"api", "db_v3"}},
		{
			// Rotation midway: a service still runs the old version.
			"an older version in use stays",
			func(id string) bool { return id == "db_v2" },
			VersionSelection{},
			[]string{"api", "db_v3", "db_v2"},
		},
		{
			// Two keys live side by side: every version a service runs stays.
			"every older version in use stays",
			func(id string) bool { return id == "db_v1" || id == "db_v2" },
			VersionSelection{},
			[]string{"db_v1", "api", "db_v3", "db_v2"},
		},
		{"all", unused, VersionSelection{All: true}, []string{"db_v1", "api", "db_v3", "db_v2"}},
		{
			"one group, newest first",
			unused,
			VersionSelection{Of: "db_v1"},
			[]string{"db_v3", "db_v2", "db_v1"},
		},
		{
			"an ungrouped item is its own group",
			unused,
			VersionSelection{Of: "api"},
			[]string{"api"},
		},
		{"an unknown ID names nothing", unused, VersionSelection{Of: "gone"}, nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			groups := SecretVersionGroups(secrets, test.inUse)

			var got []string
			for _, s := range SelectVersions(secrets, id, groups, test.sel) {
				got = append(got, s.ID)
			}

			if !slices.Equal(got, test.want) {
				t.Errorf("SelectVersions() = %v, want %v", got, test.want)
			}
		})
	}

	// db_v2 in use is shown, so the newest counts only db_v1 as hidden.
	groups := SecretVersionGroups(secrets, func(id string) bool { return id == "db_v2" })
	if v := groups.Version("db_v3"); !v.Newest || v.OlderVersions != 1 || v.Of != "db" {
		t.Errorf("Version(db_v3) = %+v, want one hidden older version", v)
	}
	if v := groups.Version("db_v2"); v.Newest || !v.InUse {
		t.Errorf("Version(db_v2) = %+v, want an older version in use", v)
	}
	if v := groups.Version("api"); v != (Version{Newest: true}) {
		t.Errorf("Version(api) = %+v, want its own newest version", v)
	}
}

// A previous spec alone keeps no older version shown: only a rollback needs it.
func TestSecretVersionViewShowsOnlyCurrentUse(t *testing.T) {
	c := cache.New(nil)
	secrets := []swarm.Secret{
		versionedSecret("db_v1", 1), versionedSecret("db_v2", 2), versionedSecret("db_v3", 3),
	}
	for _, sec := range secrets {
		c.SetSecret(sec)
	}

	c.SetService(swarm.Service{
		ID: "svc",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "api"},
			TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{
				Secrets: []*swarm.SecretReference{{SecretID: "db_v2"}},
			}},
		},
		PreviousSpec: &swarm.ServiceSpec{TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Secrets: []*swarm.SecretReference{{SecretID: "db_v1"}},
			},
		}},
	})

	view := SecretVersionView(c, secrets, func(string) bool { return true })

	var shown []string
	for _, sec := range VersionedSecrets(view, secrets, VersionSelection{}) {
		shown = append(shown, sec.ID)
	}

	if !slices.Equal(shown, []string{"db_v2", "db_v3"}) {
		t.Errorf("shown = %v, want db_v2 in use and the newest", shown)
	}
}

func TestParseVersionSelection(t *testing.T) {
	tests := []struct {
		value     string
		narrowing Narrowing
		all       bool
	}{
		{"", Narrowing{}, false},
		{"newest", Narrowing{}, false},
		{"all", Narrowing{}, true},
		// A search, filter or label finds an older version unless asked not to.
		{"", Narrowing{Search: "db"}, true},
		{"", Narrowing{Filter: `name == "db"`}, true},
		{"", Narrowing{Label: "team"}, true},
		{"newest", Narrowing{Search: "db"}, false},
	}

	for _, test := range tests {
		sel, err := ParseVersionSelection(test.value, "", test.narrowing)
		if err != nil || sel.All != test.all {
			t.Errorf(
				"ParseVersionSelection(%q, %+v) = %+v, %v",
				test.value,
				test.narrowing,
				sel,
				err,
			)
		}
	}

	if _, err := ParseVersionSelection("latest", "", Narrowing{}); err == nil {
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

// Repointing a service from v1 to v2 changes which older versions are in use,
// so v3's digest, which counts them, hears of it too.
func TestVersionIndexFollowsAReferenceChange(t *testing.T) {
	c := cache.New(nil)
	for i, name := range []string{"db_v1", "db_v2", "db_v3"} {
		c.SetSecret(versionedSecret(name, i+1))
	}

	using := func(id string) swarm.Service {
		return swarm.Service{
			ID: "svc",
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: "api"},
				TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{
					Secrets: []*swarm.SecretReference{{SecretID: id}},
				}},
			},
		}
	}
	c.SetService(using("db_v1"))

	var changed []cache.Event
	c.AddOnChangeListener(func(e cache.Event) {
		if e.Action == "ref_changed" {
			changed = append(changed, e)
		}
	})
	c.SetService(using("db_v2"))

	index := NewVersionIndex(c)
	if len(changed) == 0 ||
		!slices.ContainsFunc(changed, func(e cache.Event) bool {
			return index.TouchesDigest(e, cache.EventSecret, "db_v3")
		}) {
		t.Errorf("no ref_changed of %+v touches db_v3", changed)
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
