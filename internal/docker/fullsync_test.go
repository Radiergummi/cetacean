package docker

import "testing"

// A worker answers its node-local networks and volumes and refuses every
// swarm list, which must not read as a healthy, empty cluster.
func TestFullSyncError(t *testing.T) {
	cases := []struct {
		name    string
		failed  []string
		wantErr bool
	}{
		{"nothing failed", nil, false},
		{"one swarm list failed", []string{"tasks"}, false},
		{"a worker node", []string{"nodes", "services", "tasks", "configs", "secrets"}, true},
		{
			"everything failed",
			[]string{"nodes", "services", "tasks", "configs", "secrets", "networks", "volumes"},
			true,
		},
	}
	for _, tc := range cases {
		failed := make(map[string]bool)
		for _, name := range tc.failed {
			failed[name] = true
		}

		if err := fullSyncError(failed); (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
	}
}
