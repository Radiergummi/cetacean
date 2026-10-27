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

// Version reports where id sits in g. An ungrouped item is its own newest.
func (g VersionGroups) Version(id string) Version {
	m, ok := g[id]
	if !ok {
		return Version{Newest: true}
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
		if canRead(ServiceRefACLResource(ref)) {
			n++
		}
	}

	return n
}

// VersionView is one caller's view of a config or secret listing: the groups
// among what it may read, and the services it may read that use each item.
type VersionView struct {
	Groups  VersionGroups
	users   func(id string) []cache.ServiceRef
	canRead func(resource string) bool
}

// ConfigVersionView groups the readable configs for a caller.
func ConfigVersionView(
	c *cache.Cache,
	readable []swarm.Config,
	canRead func(resource string) bool,
) VersionView {
	return VersionView{ConfigVersionGroups(readable), c.ServicesUsingConfig, canRead}
}

// SecretVersionView groups the readable secrets for a caller.
func SecretVersionView(
	c *cache.Cache,
	readable []swarm.Secret,
	canRead func(resource string) bool,
) VersionView {
	return VersionView{SecretVersionGroups(readable), c.ServicesUsingSecret, canRead}
}

// Fields is what the item with id adds to a listing.
func (v VersionView) Fields(id string) VersionFields {
	return v.Groups.Version(id).Fields(ReadableUsers(v.users(id), v.canRead))
}

// VersionedConfigs is the listing sel selects from readable, with each
// config's version fields.
func VersionedConfigs(
	v VersionView,
	readable []swarm.Config,
	sel VersionSelection,
) []VersionedConfig {
	shown := SelectVersions(readable, func(c swarm.Config) string { return c.ID }, v.Groups, sel)
	out := make([]VersionedConfig, len(shown))
	for i, cfg := range shown {
		out[i] = VersionedConfig{Config: cfg, VersionFields: v.Fields(cfg.ID)}
	}

	return out
}

// VersionedSecrets is VersionedConfigs for secrets.
func VersionedSecrets(
	v VersionView,
	readable []swarm.Secret,
	sel VersionSelection,
) []VersionedSecret {
	shown := SelectVersions(readable, func(s swarm.Secret) string { return s.ID }, v.Groups, sel)
	out := make([]VersionedSecret, len(shown))
	for i, sec := range shown {
		out[i] = VersionedSecret{Secret: sec, VersionFields: v.Fields(sec.ID)}
	}

	return out
}

// AnnotateVersionRows places config or secret rows among their versions.
func AnnotateVersionRows(rows []Row, v VersionView) {
	for i := range rows {
		f := v.Fields(rows[i].ID)
		rows[i].VersionOf, rows[i].VersionInferred = f.VersionOf, f.VersionInferred
		rows[i].NewestVersion, rows[i].OlderVersions = f.NewestVersion, f.OlderVersions
		rows[i].UsedBy = new(f.UsedBy)
	}
}

// VersionRef names one config or secret in a VersionIndex.
type VersionRef struct{ ID, Name string }

// VersionIndex holds every config's and secret's group, rebuilt once per cache
// generation, so one item's siblings are found without matching every name in
// the cluster on each read.
type VersionIndex struct {
	cache *cache.Cache

	mu         sync.Mutex
	built      bool
	generation uint64
	configs    map[versions.GroupKey][]VersionRef
	secrets    map[versions.GroupKey][]VersionRef
}

// NewVersionIndex indexes c's configs and secrets on first use.
func NewVersionIndex(c *cache.Cache) *VersionIndex { return &VersionIndex{cache: c} }

func groupsOf(items []versions.Item) map[versions.GroupKey][]VersionRef {
	groups := map[versions.GroupKey][]VersionRef{}
	for _, it := range items {
		key := versions.Key(it)
		groups[key] = append(groups[key], VersionRef{it.ID, it.Name})
	}

	return groups
}

func (x *VersionIndex) current() (configs, secrets map[versions.GroupKey][]VersionRef) {
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

	x.configs, x.secrets = groupsOf(configItems), groupsOf(secretItems)
	x.built, x.generation = true, generation

	return x.configs, x.secrets
}

// ConfigVersion is the Version of one config, grouped with only the readable
// configs sharing its key.
func (x *VersionIndex) ConfigVersion(cfg swarm.Config, canRead func(resource string) bool) Version {
	configs, _ := x.current()
	siblings := []swarm.Config{cfg}

	for _, ref := range configs[versions.Key(versions.ConfigItem(cfg))] {
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

	for _, ref := range secrets[versions.Key(versions.SecretItem(sec))] {
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
// group, and in the group an update took it out of. A service's references
// need nothing here: the cache sends ref_changed for each one gained or lost.
func (x *VersionIndex) DigestsTouchedBy(event cache.Event) (configs, secrets []VersionRef) {
	switch resource := event.Resource.(type) {
	case swarm.Config:
		all, _ := x.current()
		keys := []versions.GroupKey{versions.Key(versions.ConfigItem(resource))}
		if previous, ok := event.Previous.(swarm.Config); ok {
			keys = append(keys, versions.Key(versions.ConfigItem(previous)))
		}

		return touched(all, resource.ID, keys), nil

	case swarm.Secret:
		_, all := x.current()
		keys := []versions.GroupKey{versions.Key(versions.SecretItem(resource))}
		if previous, ok := event.Previous.(swarm.Secret); ok {
			keys = append(keys, versions.Key(versions.SecretItem(previous)))
		}

		return nil, touched(all, resource.ID, keys)

	default:
		return nil, nil
	}
}

// TouchesDigest reports whether event changes the digest of the config or
// secret typ and id name, by what DigestsTouchedBy finds.
func (x *VersionIndex) TouchesDigest(event cache.Event, typ cache.EventType, id string) bool {
	if event.Type != cache.EventConfig && event.Type != cache.EventSecret {
		return false
	}

	configs, secrets := x.DigestsTouchedBy(event)

	refs := configs
	if typ == cache.EventSecret {
		refs = secrets
	}

	return slices.ContainsFunc(refs, func(ref VersionRef) bool { return ref.ID == id })
}

// touched is every member but id of the groups keys name, each group once.
func touched(
	groups map[versions.GroupKey][]VersionRef,
	id string,
	keys []versions.GroupKey,
) []VersionRef {
	var out []VersionRef
	for i, key := range keys {
		if slices.Contains(keys[:i], key) {
			continue
		}
		for _, ref := range groups[key] {
			if ref.ID != id {
				out = append(out, ref)
			}
		}
	}

	return out
}
