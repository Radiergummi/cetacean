package cluster

import (
	"errors"
	"slices"
	"sync"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/versions"
)

// Version is a config's or secret's place among its versions, as one caller
// sees them: grouped only with versions it may read, so its newest readable
// version is the one it calls newest.
type Version struct {
	Of            string
	Inferred      bool
	Newest        bool
	OlderVersions int
}

// VersionFields is what a config or secret listing adds to each Docker record,
// spelled as the record's own fields are.
type VersionFields struct {
	VersionOf       string `json:"VersionOf,omitempty"`
	VersionInferred bool   `json:"VersionInferred,omitempty"`
	NewestVersion   bool   `json:"NewestVersion,omitempty"`
	OlderVersions   int    `json:"OlderVersions,omitempty"`
	UsedBy          int    `json:"UsedBy"`
}

// Fields spells v for a listing, with usedBy readable services using it.
func (v Version) Fields(usedBy int) VersionFields {
	return VersionFields{
		VersionOf:       v.Of,
		VersionInferred: v.Inferred,
		NewestVersion:   v.Newest,
		OlderVersions:   v.OlderVersions,
		UsedBy:          usedBy,
	}
}

// VersionedConfig is a config as a listing shows it.
type VersionedConfig struct {
	swarm.Config
	VersionFields
}

// VersionedSecret is a secret as a listing shows it.
type VersionedSecret struct {
	swarm.Secret
	VersionFields
}

// VersionGroups is the grouping of the configs or secrets one caller may read.
type VersionGroups map[string]versions.Membership

// ConfigVersionGroups groups the configs a caller may read.
func ConfigVersionGroups(readable []swarm.Config) VersionGroups {
	return versions.Group(versions.ConfigItems(readable))
}

// SecretVersionGroups groups the secrets a caller may read.
func SecretVersionGroups(readable []swarm.Secret) VersionGroups {
	return versions.Group(versions.SecretItems(readable))
}

// Version reports where id sits in g; the zero Version for an ungrouped item.
func (g VersionGroups) Version(id string) Version {
	m, ok := g[id]
	if !ok {
		return Version{}
	}

	v := Version{Of: m.Of, Inferred: m.Inferred, Newest: m.Rank == 0}
	if v.Newest {
		v.OlderVersions = m.Count - 1
	}

	return v
}

// VersionSelection is which versions a listing shows: by default only each
// group's newest, every one with All, and with Of only the group holding the
// item with that ID, newest first.
type VersionSelection struct {
	All bool
	Of  string
}

// ErrUnknownVersions is a versions value that is neither "newest" nor "all".
var ErrUnknownVersions = errors.New(`versions must be "newest" or "all"`)

// ParseVersionSelection reads the versions and versionsOf arguments. A search
// or filter shows every version it matches unless versions says otherwise, so
// an older version can still be found by name.
func ParseVersionSelection(versionsArg, of string, searching bool) (VersionSelection, error) {
	switch versionsArg {
	case "":
		return VersionSelection{All: searching, Of: of}, nil
	case "newest":
		return VersionSelection{Of: of}, nil
	case "all":
		return VersionSelection{All: true, Of: of}, nil
	default:
		return VersionSelection{}, ErrUnknownVersions
	}
}

// SelectVersions keeps the items sel shows out of the readable items g was
// grouped from, as a new slice. An ungrouped item is its own newest version.
func SelectVersions[T any](
	items []T,
	id func(T) string,
	g VersionGroups,
	sel VersionSelection,
) []T {
	var out []T

	if sel.Of != "" {
		target, grouped := g[sel.Of]
		for _, item := range items {
			m, ok := g[id(item)]
			if id(item) == sel.Of || grouped && ok && m.Of == target.Of && m.Stack == target.Stack {
				out = append(out, item)
			}
		}

		slices.SortStableFunc(out, func(a, b T) int { return g[id(a)].Rank - g[id(b)].Rank })

		return out
	}

	for _, item := range items {
		if sel.All || g[id(item)].Rank == 0 {
			out = append(out, item)
		}
	}

	return out
}

// ReadableUsers counts the services among refs canRead allows.
func ReadableUsers(refs []cache.ServiceRef, canRead func(resource string) bool) int {
	n := 0
	for _, ref := range refs {
		if canRead("service:" + ref.Name) {
			n++
		}
	}

	return n
}

// AnnotateVersionRows places config or secret rows among their versions in g
// and counts the readable services users names for each.
func AnnotateVersionRows(
	rows []Row,
	g VersionGroups,
	users func(id string) []cache.ServiceRef,
	canRead func(resource string) bool,
) {
	for i := range rows {
		v := g.Version(rows[i].ID)
		rows[i].VersionOf, rows[i].VersionInferred = v.Of, v.Inferred
		rows[i].NewestVersion, rows[i].OlderVersions = v.Newest, v.OlderVersions
		rows[i].UsedBy = new(ReadableUsers(users(rows[i].ID), canRead))
	}
}

// VersionRef names one config or secret in a VersionIndex.
type VersionRef struct{ ID, Name string }

// kindIndex is the index of one kind: each group's members, each member's
// group, and the services each one is used by.
type kindIndex struct {
	groups map[versions.GroupKey][]VersionRef
	keys   map[string]versions.GroupKey
	users  versions.Users
}

func indexOf(items []versions.Item, users versions.Users) kindIndex {
	index := kindIndex{
		groups: map[versions.GroupKey][]VersionRef{},
		keys:   make(map[string]versions.GroupKey, len(items)),
		users:  users,
	}

	for _, it := range items {
		key := versions.Key(it)
		index.groups[key] = append(index.groups[key], VersionRef{it.ID, it.Name})
		index.keys[it.ID] = key
	}

	return index
}

// VersionIndex holds every config's and secret's group key and users, rebuilt
// once per cache generation, so one item's siblings are found without matching
// every name in the cluster on each read. It keeps the keys of the build before
// too, so a version that moved groups can be found in the one it left.
type VersionIndex struct {
	cache *cache.Cache

	mu         sync.Mutex
	built      bool
	generation uint64
	configs    kindIndex
	secrets    kindIndex
	previous   struct{ configs, secrets map[string]versions.GroupKey }
}

// NewVersionIndex indexes c's configs and secrets on first use.
func NewVersionIndex(c *cache.Cache) *VersionIndex { return &VersionIndex{cache: c} }

func (x *VersionIndex) current() (configs, secrets kindIndex) {
	x.mu.Lock()
	defer x.mu.Unlock()

	generation := x.cache.Generation()
	if x.built && x.generation == generation {
		return x.configs, x.secrets
	}

	var configItems, secretItems []versions.Item
	x.cache.EachConfig(func(c swarm.Config) bool {
		configItems = append(configItems, versions.ConfigItem(c))
		return true
	})
	x.cache.EachSecret(func(s swarm.Secret) bool {
		secretItems = append(secretItems, versions.SecretItem(s))
		return true
	})
	configUsers, secretUsers := versions.UsersOf(x.cache.EachService)

	x.previous.configs, x.previous.secrets = x.configs.keys, x.secrets.keys
	x.configs = indexOf(configItems, configUsers)
	x.secrets = indexOf(secretItems, secretUsers)
	x.built, x.generation = true, generation

	return x.configs, x.secrets
}

// ConfigUsers is the services that need a config: those whose spec or previous
// spec references it, since a rollback needs the version it restores.
func (x *VersionIndex) ConfigUsers(id string) []cache.ServiceRef {
	configs, _ := x.current()
	return configs.users[id]
}

// SecretUsers is ConfigUsers for a secret.
func (x *VersionIndex) SecretUsers(id string) []cache.ServiceRef {
	_, secrets := x.current()
	return secrets.users[id]
}

// ConfigVersion is the Version of one config, grouped with only the readable
// configs sharing its key.
func (x *VersionIndex) ConfigVersion(cfg swarm.Config, canRead func(resource string) bool) Version {
	configs, _ := x.current()
	siblings := []swarm.Config{cfg}

	for _, ref := range configs.groups[versions.Key(versions.ConfigItem(cfg))] {
		if ref.ID == cfg.ID || !canRead("config:"+ref.Name) {
			continue
		}
		if other, ok := x.cache.GetConfig(ref.ID); ok {
			siblings = append(siblings, other)
		}
	}

	return ConfigVersionGroups(siblings).Version(cfg.ID)
}

// SecretVersion is ConfigVersion for a secret.
func (x *VersionIndex) SecretVersion(sec swarm.Secret, canRead func(resource string) bool) Version {
	_, secrets := x.current()
	siblings := []swarm.Secret{sec}

	for _, ref := range secrets.groups[versions.Key(versions.SecretItem(sec))] {
		if ref.ID == sec.ID || !canRead("secret:"+ref.Name) {
			continue
		}
		if other, ok := x.cache.GetSecret(ref.ID); ok {
			siblings = append(siblings, other)
		}
	}

	return SecretVersionGroups(siblings).Version(sec.ID)
}

// DigestsTouchedBy names the configs and secrets whose digest event changes
// besides any it names itself: the other versions in a config's or secret's
// group, and in the group a relabel took it out of. A service's references
// need nothing here: the cache sends ref_changed for each one gained or lost.
func (x *VersionIndex) DigestsTouchedBy(event cache.Event) (configs, secrets []VersionRef) {
	switch resource := event.Resource.(type) {
	case swarm.Config:
		all, _ := x.current()
		key := versions.Key(versions.ConfigItem(resource))

		return x.touched(all, x.previous.configs, resource.ID, key), nil

	case swarm.Secret:
		_, all := x.current()
		key := versions.Key(versions.SecretItem(resource))

		return nil, x.touched(all, x.previous.secrets, resource.ID, key)

	default:
		return nil, nil
	}
}

func (x *VersionIndex) touched(
	index kindIndex,
	previous map[string]versions.GroupKey,
	id string,
	key versions.GroupKey,
) []VersionRef {
	x.mu.Lock()
	left, moved := previous[id]
	x.mu.Unlock()

	refs := others(index.groups[key], id)
	if moved && left != key {
		refs = append(refs, others(index.groups[left], id)...)
	}

	return refs
}

func others(refs []VersionRef, id string) []VersionRef {
	var out []VersionRef
	for _, ref := range refs {
		if ref.ID != id {
			out = append(out, ref)
		}
	}

	return out
}
