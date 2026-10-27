// Package versions groups the configs and secrets that rotation leaves behind
// into versions of one logical resource. It is a leaf so the domain layer and
// the recommendations engine share one reading of it.
package versions

import (
	"regexp"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// Label names the logical resource a config or secret is a version of.
const Label = "cetacean.version-of"

// suffix is a version stamp: a hash of eight or more hex digits, or a v-number.
// A bare short number is not one; `worker_1` and `worker_2` are siblings.
var suffix = regexp.MustCompile(`(?i)[_-](?:[0-9a-f]{8,}|v[0-9]+)$`)

// Item is what grouping reads of a config or secret.
type Item struct {
	ID      string
	Name    string
	Labels  map[string]string
	Created time.Time
}

// Membership places one item in its group. Inferred is true unless every
// member declared the group with Label.
type Membership struct {
	Of       string
	Inferred bool
	Newest   bool
}

type key struct{ stack, name string }

// Group returns the membership of every item that has at least one other
// version in the same stack, keyed by ID. An item's logical name is its Label,
// or else its name with any version suffix removed.
func Group(items []Item) map[string]Membership {
	groups := map[key][]Item{}
	for _, it := range items {
		name, ok := it.Labels[Label]
		if !ok || name == "" {
			name = suffix.ReplaceAllString(it.Name, "")
		}
		k := key{it.Labels[cache.StackLabel], name}
		groups[k] = append(groups[k], it)
	}

	out := map[string]Membership{}
	for k, members := range groups {
		if len(members) < 2 {
			continue
		}

		inferred := false
		newest := members[0]
		for _, m := range members {
			if m.Labels[Label] == "" {
				inferred = true
			}
			if m.Created.After(newest.Created) ||
				(m.Created.Equal(newest.Created) && m.ID > newest.ID) {
				newest = m
			}
		}

		for _, m := range members {
			out[m.ID] = Membership{Of: k.name, Inferred: inferred, Newest: m.ID == newest.ID}
		}
	}

	return out
}

// Secrets groups secrets by Group.
func Secrets(secrets []swarm.Secret) map[string]Membership {
	items := make([]Item, len(secrets))
	for i, s := range secrets {
		items[i] = Item{s.ID, s.Spec.Name, s.Spec.Labels, s.CreatedAt}
	}

	return Group(items)
}

// Configs groups configs by Group.
func Configs(configs []swarm.Config) map[string]Membership {
	items := make([]Item, len(configs))
	for i, c := range configs {
		items[i] = Item{c.ID, c.Spec.Name, c.Spec.Labels, c.CreatedAt}
	}

	return Group(items)
}
