// Package versions groups the configs and secrets that rotation leaves behind
// into versions of one logical resource. It sits below internal/cluster, which
// imports the recommendations engine, so both share one reading of it.
package versions

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
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

// Membership places one item in its group of Count versions. Rank orders the
// group newest first, so the newest is 0. Inferred is true unless every member
// declared the group with Label.
type Membership struct {
	Of       string
	Stack    string
	Inferred bool
	Rank     int
	Count    int
}

// GroupKey identifies a group: the stack an item belongs to, and its logical
// name within it.
type GroupKey struct{ Stack, Name string }

// Key is an item's group: its Label, or else its name, either way without a
// version suffix and without the `<stack>_` or `<stack>-` prefix a stack deploy
// writes. An item joins only the groups of its own stack.
func Key(it Item) GroupKey {
	stack := it.Labels[cache.StackLabel]

	name := it.Labels[Label]
	if name == "" {
		name = it.Name
	}

	name = suffix.ReplaceAllString(name, "")
	if stack != "" {
		for _, separator := range []string{"_", "-"} {
			if rest, ok := strings.CutPrefix(name, stack+separator); ok && rest != "" {
				name = rest
				break
			}
		}
	}

	return GroupKey{stack, name}
}

// Group returns the membership of every item that has at least one other
// version, keyed by ID. The newest is the last created, the greater ID on a tie.
func Group(items []Item) map[string]Membership {
	groups := map[GroupKey][]Item{}
	for _, it := range items {
		k := Key(it)
		groups[k] = append(groups[k], it)
	}

	out := map[string]Membership{}
	for k, members := range groups {
		if len(members) < 2 {
			continue
		}

		slices.SortFunc(members, func(a, b Item) int {
			if c := b.Created.Compare(a.Created); c != 0 {
				return c
			}
			return cmp.Compare(b.ID, a.ID)
		})

		inferred := slices.ContainsFunc(members, func(m Item) bool { return m.Labels[Label] == "" })
		for rank, m := range members {
			out[m.ID] = Membership{
				Of:       k.Name,
				Stack:    k.Stack,
				Inferred: inferred,
				Rank:     rank,
				Count:    len(members),
			}
		}
	}

	return out
}

// SecretItem is what grouping reads of a secret.
func SecretItem(s swarm.Secret) Item {
	return Item{s.ID, s.Spec.Name, s.Spec.Labels, s.CreatedAt}
}

// ConfigItem is what grouping reads of a config.
func ConfigItem(c swarm.Config) Item {
	return Item{c.ID, c.Spec.Name, c.Spec.Labels, c.CreatedAt}
}

// SecretItems is SecretItem over a slice.
func SecretItems(secrets []swarm.Secret) []Item {
	items := make([]Item, len(secrets))
	for i, s := range secrets {
		items[i] = SecretItem(s)
	}

	return items
}

// ConfigItems is ConfigItem over a slice.
func ConfigItems(configs []swarm.Config) []Item {
	items := make([]Item, len(configs))
	for i, c := range configs {
		items[i] = ConfigItem(c)
	}

	return items
}

// Secrets groups secrets by Group.
func Secrets(secrets []swarm.Secret) map[string]Membership {
	return Group(SecretItems(secrets))
}

// Configs groups configs by Group.
func Configs(configs []swarm.Config) map[string]Membership {
	return Group(ConfigItems(configs))
}
