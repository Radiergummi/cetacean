package cluster

import "github.com/docker/docker/api/types/swarm"

// searchACLPrefix maps Search's plural type keys to their ACL resource prefix.
// Explicit so an irregular plural ("policies") cannot produce a wrong key.
var searchACLPrefix = map[string]string{
	"services": "service:",
	"stacks":   "stack:",
	"nodes":    "node:",
	"tasks":    "task:",
	"configs":  "config:",
	"secrets":  "secret:",
	"networks": "network:",
	"volumes":  "volume:",
}

// SearchResultACLResource returns the ACL resource a search hit under
// resourceType is authorized as. Tasks key on the task ID, every other type on
// the resource name.
func SearchResultACLResource(resourceType string, sr SearchResult) string {
	prefix := searchACLPrefix[resourceType]
	if resourceType == "tasks" {
		return prefix + sr.ID
	}

	return prefix + sr.Name
}

// NodeACLName returns the name a node is authorized as: its hostname, or its
// ID when it has none.
func NodeACLName(n swarm.Node) string {
	if n.Description.Hostname != "" {
		return n.Description.Hostname
	}

	return n.ID
}
