package cluster

import (
	"errors"
	"slices"

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

// VersionGroups is the grouping of the configs or secrets one caller may read.
type VersionGroups map[string]versions.Membership

// ConfigVersionGroups groups the configs a caller may read.
func ConfigVersionGroups(readable []swarm.Config) VersionGroups {
	return versions.Configs(readable)
}

// SecretVersionGroups groups the secrets a caller may read.
func SecretVersionGroups(readable []swarm.Secret) VersionGroups {
	return versions.Secrets(readable)
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

// ParseVersionSelection reads the versions and versionsOf arguments.
func ParseVersionSelection(versionsArg, of string) (VersionSelection, error) {
	switch versionsArg {
	case "", "newest":
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

// ConfigVersion is the Version of one config, grouped with only the readable
// configs sharing its key rather than with every config there is.
func ConfigVersion(c *cache.Cache, cfg swarm.Config, canRead func(resource string) bool) Version {
	key := versions.Key(versions.ConfigItem(cfg))
	siblings := []swarm.Config{cfg}

	c.EachConfig(func(other swarm.Config) bool {
		if other.ID != cfg.ID && versions.Key(versions.ConfigItem(other)) == key &&
			canRead("config:"+other.Spec.Name) {
			siblings = append(siblings, other)
		}

		return true
	})

	return ConfigVersionGroups(siblings).Version(cfg.ID)
}

// SecretVersion is ConfigVersion for a secret.
func SecretVersion(c *cache.Cache, sec swarm.Secret, canRead func(resource string) bool) Version {
	key := versions.Key(versions.SecretItem(sec))
	siblings := []swarm.Secret{sec}

	c.EachSecret(func(other swarm.Secret) bool {
		if other.ID != sec.ID && versions.Key(versions.SecretItem(other)) == key &&
			canRead("secret:"+other.Spec.Name) {
			siblings = append(siblings, other)
		}

		return true
	})

	return SecretVersionGroups(siblings).Version(sec.ID)
}
