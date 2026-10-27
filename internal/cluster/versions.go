package cluster

import (
	"errors"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/versions"
)

// Version is a config's or secret's place among its versions, as one caller
// sees them: grouped only with versions it may read, so its newest readable
// version is the one it calls newest. InUse marks an older version a service's
// current spec uses; OlderVersions counts, on the newest, the older ones not in use.
type Version struct {
	Of            string
	Inferred      bool
	Newest        bool
	InUse         bool
	OlderVersions int
}

// VersionFields is what a config or secret listing adds to each Docker record,
// spelled as the record's own fields are.
type VersionFields struct {
	VersionOf       string `json:"VersionOf,omitempty"`
	VersionInferred bool   `json:"VersionInferred,omitempty"`
	NewestVersion   bool   `json:"NewestVersion,omitempty"`
	InUse           bool   `json:"InUse,omitempty"`
	OlderVersions   int    `json:"OlderVersions,omitempty"`
	UsedBy          int    `json:"UsedBy"`
}

// Fields spells v for a listing, with usedBy readable services using it.
func (v Version) Fields(usedBy int) VersionFields {
	return VersionFields{
		VersionOf:       v.Of,
		VersionInferred: v.Inferred,
		NewestVersion:   v.Newest,
		InUse:           v.InUse,
		OlderVersions:   v.OlderVersions,
		UsedBy:          usedBy,
	}
}

// VersionDetail is VersionFields as a config's or secret's detail spells it.
type VersionDetail struct {
	VersionOf       string `json:"versionOf,omitempty"`
	VersionInferred bool   `json:"versionInferred,omitempty"`
	NewestVersion   bool   `json:"newestVersion,omitempty"`
	InUse           bool   `json:"inUse,omitempty"`
	OlderVersions   int    `json:"olderVersions,omitempty"`
	UsedBy          int    `json:"usedBy"`
}

// Detail spells v for a detail, with usedBy readable services using it.
func (v Version) Detail(usedBy int) VersionDetail { return VersionDetail(v.Fields(usedBy)) }

// Map is d as a map, holding exactly the keys its JSON does, for a detail
// that is a map rather than a struct.
func (d VersionDetail) Map() map[string]any {
	out := map[string]any{"usedBy": d.UsedBy}
	if d.VersionOf != "" {
		out["versionOf"] = d.VersionOf
	}
	if d.VersionInferred {
		out["versionInferred"] = true
	}
	if d.NewestVersion {
		out["newestVersion"] = true
	}
	if d.InUse {
		out["inUse"] = true
	}
	if d.OlderVersions != 0 {
		out["olderVersions"] = d.OlderVersions
	}

	return out
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

// VersionGroups is the grouping of the configs or secrets one caller may read,
// with which older versions a current spec keeps in use.
type VersionGroups struct {
	members map[string]versions.Membership
	inUse   map[string]bool
	hidden  map[versions.GroupKey]int
}

func groupVersions(items []versions.Item, inUse func(id string) bool) VersionGroups {
	g := VersionGroups{
		members: versions.Group(items),
		inUse:   map[string]bool{},
		hidden:  map[versions.GroupKey]int{},
	}

	for id, m := range g.members {
		if m.Rank == 0 {
			continue
		}
		if inUse(id) {
			g.inUse[id] = true
		} else {
			g.hidden[versions.GroupKey{Stack: m.Stack, Name: m.Of}]++
		}
	}

	return g
}

// ConfigVersionGroups groups the configs a caller may read; inUse says which a
// service's current spec uses.
func ConfigVersionGroups(readable []swarm.Config, inUse func(id string) bool) VersionGroups {
	return groupVersions(versions.ConfigItems(readable), inUse)
}

// SecretVersionGroups is ConfigVersionGroups for secrets.
func SecretVersionGroups(readable []swarm.Secret, inUse func(id string) bool) VersionGroups {
	return groupVersions(versions.SecretItems(readable), inUse)
}

// Version reports where id sits in g. An ungrouped item is its own newest.
func (g VersionGroups) Version(id string) Version {
	m, ok := g.members[id]
	if !ok {
		return Version{Newest: true}
	}

	v := Version{Of: m.Of, Inferred: m.Inferred, Newest: m.Rank == 0, InUse: g.inUse[id]}
	if v.Newest {
		v.OlderVersions = g.hidden[versions.GroupKey{Stack: m.Stack, Name: m.Of}]
	}

	return v
}

// shown reports whether a listing shows id by default: the newest of its group,
// an older version still in use, or an item with no other version.
func (g VersionGroups) shown(id string) bool {
	return g.members[id].Rank == 0 || g.inUse[id]
}

// VersionSelection is which versions a listing shows: by default each group's
// newest and the older ones still in use, every one with All, and with Of only
// the group holding the item with that ID, newest first.
type VersionSelection struct {
	All bool
	Of  string
}

// Narrowing is how a request narrows a config or secret listing. A free-text
// search, a filter expression or a label filter can find an older version by
// name, so it lists every version it matches; a stack, state, node or image
// narrowing does not.
type Narrowing struct {
	Search, Filter, Label string
}

func (n Narrowing) findsOlderVersions() bool {
	return n.Search != "" || n.Filter != "" || n.Label != ""
}

// ErrUnknownVersions is a versions value that is neither "newest" nor "all".
var ErrUnknownVersions = errors.New(`versions must be "newest" or "all"`)

// ParseVersionSelection reads the versions and versionsOf arguments for a
// listing narrowed by n.
func ParseVersionSelection(versionsArg, of string, n Narrowing) (VersionSelection, error) {
	switch versionsArg {
	case "":
		return VersionSelection{All: n.findsOlderVersions(), Of: of}, nil
	case "newest":
		return VersionSelection{Of: of}, nil
	case "all":
		return VersionSelection{All: true, Of: of}, nil
	default:
		return VersionSelection{}, ErrUnknownVersions
	}
}

// SelectVersions keeps the items sel shows out of the readable items g was
// grouped from, as a new slice.
func SelectVersions[T any](
	items []T,
	id func(T) string,
	g VersionGroups,
	sel VersionSelection,
) []T {
	var out []T

	if sel.Of != "" {
		target, grouped := g.members[sel.Of]
		for _, item := range items {
			m, ok := g.members[id(item)]
			if id(item) == sel.Of || grouped && ok && m.Of == target.Of && m.Stack == target.Stack {
				out = append(out, item)
			}
		}

		slices.SortStableFunc(out, func(a, b T) int {
			return g.members[id(a)].Rank - g.members[id(b)].Rank
		})

		return out
	}

	for _, item := range items {
		if sel.All || g.shown(id(item)) {
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

// inCurrentUse says whether any readable service among users holds an item in
// its current spec; one a previous spec alone names keeps no version shown.
func inCurrentUse(
	users func(id string) []cache.ServiceRef,
	canRead func(resource string) bool,
) func(id string) bool {
	return func(id string) bool {
		return slices.ContainsFunc(users(id), func(ref cache.ServiceRef) bool {
			return !ref.Rollback && canRead(ServiceRefACLResource(ref))
		})
	}
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
	groups := ConfigVersionGroups(readable, inCurrentUse(c.ServicesUsingConfig, canRead))

	return VersionView{groups, c.ServicesUsingConfig, canRead}
}

// SecretVersionView groups the readable secrets for a caller.
func SecretVersionView(
	c *cache.Cache,
	readable []swarm.Secret,
	canRead func(resource string) bool,
) VersionView {
	groups := SecretVersionGroups(readable, inCurrentUse(c.ServicesUsingSecret, canRead))

	return VersionView{groups, c.ServicesUsingSecret, canRead}
}

// Fields is what the item with id adds to a listing.
func (v VersionView) Fields(id string) VersionFields {
	return v.Groups.Version(id).Fields(ReadableUsers(v.users(id), v.canRead))
}

// Configs is items with each config's version fields.
func (v VersionView) Configs(items []swarm.Config) []VersionedConfig {
	out := make([]VersionedConfig, len(items))
	for i, cfg := range items {
		out[i] = VersionedConfig{Config: cfg, VersionFields: v.Fields(cfg.ID)}
	}

	return out
}

// Secrets is items with each secret's version fields.
func (v VersionView) Secrets(items []swarm.Secret) []VersionedSecret {
	out := make([]VersionedSecret, len(items))
	for i, sec := range items {
		out[i] = VersionedSecret{Secret: sec, VersionFields: v.Fields(sec.ID)}
	}

	return out
}

// VersionedConfigs is the listing sel selects from readable.
func VersionedConfigs(
	v VersionView,
	readable []swarm.Config,
	sel VersionSelection,
) []VersionedConfig {
	return v.Configs(
		SelectVersions(readable, func(c swarm.Config) string { return c.ID }, v.Groups, sel),
	)
}

// VersionedSecrets is VersionedConfigs for secrets.
func VersionedSecrets(
	v VersionView,
	readable []swarm.Secret,
	sel VersionSelection,
) []VersionedSecret {
	return v.Secrets(
		SelectVersions(readable, func(s swarm.Secret) string { return s.ID }, v.Groups, sel),
	)
}

// AnnotateVersionRows places config or secret rows among their versions.
func AnnotateVersionRows(rows []Row, v VersionView) {
	for i := range rows {
		f := v.Fields(rows[i].ID)
		rows[i].VersionOf, rows[i].VersionInferred = f.VersionOf, f.VersionInferred
		rows[i].NewestVersion, rows[i].InUse = f.NewestVersion, f.InUse
		rows[i].OlderVersions = f.OlderVersions
		rows[i].UsedBy = new(f.UsedBy)
	}
}

// VersionRef names one config or secret in a VersionIndex.
type VersionRef struct{ ID, Name string }

// VersionIndex holds every config's and secret's group, rebuilt only once a
// config or secret is set or removed (cache.VersionGeneration), so one item's
// siblings are found without matching every name in the cluster on each read.
type VersionIndex struct {
	cache *cache.Cache

	mu         sync.Mutex
	built      bool
	generation uint64
	configs    map[versions.GroupKey][]VersionRef
	secrets    map[versions.GroupKey][]VersionRef

	// touched is the last event's answer: every stream and subscription asks
	// about the same event in turn, so it is worked out once.
	touched atomic.Pointer[touchedMemo]
}

// touchedMemo keys on more than the history ID, which a resync shares with
// the change that follows it.
type touchedMemo struct {
	historyID        uint64
	typ              cache.EventType
	id, action       string
	configs, secrets []VersionRef
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

	generation := x.cache.VersionGeneration()
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

	inUse := inCurrentUse(x.cache.ServicesUsingConfig, canRead)

	return ConfigVersionGroups(siblings, inUse).Version(cfg.ID)
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

	inUse := inCurrentUse(x.cache.ServicesUsingSecret, canRead)

	return SecretVersionGroups(siblings, inUse).Version(sec.ID)
}

// DigestsTouchedBy names the configs and secrets whose digest event changes
// besides any it names itself: the other versions in a config's or secret's
// group, and in the group an update took it out of. A service's references
// need nothing here: the cache sends ref_changed for each one gained or lost.
func (x *VersionIndex) DigestsTouchedBy(event cache.Event) (configs, secrets []VersionRef) {
	if event.HistoryID == 0 {
		return x.digestsTouchedBy(event)
	}

	if memo := x.touched.Load(); memo != nil && memo.historyID == event.HistoryID &&
		memo.typ == event.Type && memo.id == event.ID && memo.action == event.Action {
		return memo.configs, memo.secrets
	}

	configs, secrets = x.digestsTouchedBy(event)
	x.touched.Store(&touchedMemo{
		historyID: event.HistoryID,
		typ:       event.Type,
		id:        event.ID,
		action:    event.Action,
		configs:   configs,
		secrets:   secrets,
	})

	return configs, secrets
}

func (x *VersionIndex) digestsTouchedBy(event cache.Event) (configs, secrets []VersionRef) {
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
