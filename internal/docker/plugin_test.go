package docker

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types"
)

// What the review showed is all that is granted: the same set in another
// order passes, and anything added, removed or widened is refused.
func TestGrantOnlyAcceptsExactlyTheReviewedPrivileges(t *testing.T) {
	reviewed := types.PluginPrivileges{
		{Name: "network", Value: []string{"host"}},
		{Name: "mount", Value: []string{"/var/lib", "/etc"}},
	}

	cases := map[string]struct {
		required types.PluginPrivileges
		accepted bool
	}{
		"the same, reordered": {types.PluginPrivileges{
			{Name: "mount", Value: []string{"/etc", "/var/lib"}},
			{Name: "network", Value: []string{"host"}},
		}, true},
		"one more": {append(types.PluginPrivileges{
			{Name: "capabilities", Value: []string{"CAP_SYS_ADMIN"}},
		}, reviewed...), false},
		"a widened value": {types.PluginPrivileges{
			{Name: "network", Value: []string{"host"}},
			{Name: "mount", Value: []string{"/"}},
		}, false},
		"none": {nil, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			accepted, err := grantOnly(reviewed)(context.Background(), tc.required)
			if accepted != tc.accepted {
				t.Errorf("accepted = %v, want %v", accepted, tc.accepted)
			}
			if !tc.accepted && !errors.Is(err, ErrPluginPrivilegesChanged) {
				t.Errorf("err = %v, want ErrPluginPrivilegesChanged", err)
			}
		})
	}
}
