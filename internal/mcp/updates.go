package mcp

import (
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// The section names the spec-editing tools report, and the keys of the
// cluster.ServiceDetails / NodeDigest projection each one covers.
//
// A section is answered from the same projection describe builds rather than
// from a second one written beside it, so a caller who edits a service's ports
// and then describes it is told the same thing twice. That is also what keeps
// the answer free of what these tools must never hand back: the projection
// reports environment variable and log-driver option *names*, never their
// values, and every one of these tools used to return the whole swarm.Service
// — so a call that only raised a CPU limit came back carrying the service's
// database password.
const (
	sectionEnv            = "env"
	sectionLabels         = "labels"
	sectionResources      = "resources"
	sectionPlacement      = "placement"
	sectionPorts          = "ports"
	sectionUpdatePolicy   = "update-policy"
	sectionRollbackPolicy = "rollback-policy"
	sectionLogDriver      = "log-driver"
	sectionAvailability   = "availability"
	sectionRole           = "role"
)

// serviceSectionKeys maps a section to the cluster.ServiceDetails keys that
// answer it. A key absent from the projection — an unset resource limit, a
// service with no ports — is simply absent from the result, which is the same
// thing describe reports and is how "there is nothing set here" is said.
var serviceSectionKeys = map[string][]string{
	sectionEnv:    {"envNames"},
	sectionLabels: {"labels"},
	sectionResources: {
		"cpuLimitCores",
		"memoryLimitBytes",
		"cpuReservationCores",
		"memoryReservationBytes",
	},
	sectionPlacement:      {"placementConstraints"},
	sectionPorts:          {"ports"},
	sectionUpdatePolicy:   {"updatePolicy"},
	sectionRollbackPolicy: {"rollbackPolicy"},
	sectionLogDriver:      {"logDriver"},
}

// nodeSectionKeys is the node counterpart, over NodeDigest's details.
var nodeSectionKeys = map[string][]string{
	sectionLabels:       {"labels"},
	sectionAvailability: {"availability"},
	sectionRole:         {"role"},
}

// serviceUpdateResult is what the spec-editing service tools return: enough to
// confirm the edit landed and to address the service again, and nothing else.
//
// Version is here because a follow-up write needs it — Docker rejects an
// update carrying a stale version, and reading it back off a cache the watcher
// fills asynchronously is a race these tools would otherwise force on every
// caller. It comes off the service the write returned, which docker.Client
// ends with a fresh inspect.
type serviceUpdateResult struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Version uint64         `json:"version"`
	Section string         `json:"section"`
	Details map[string]any `json:"details"`
}

// nodeUpdateResult is the node counterpart. Role and availability ride along
// whichever section was edited, because they are the two facts that decide
// what a node will accept next: draining a node to move its work is a
// different act on a manager than on a worker, and the answer to "did the
// drain take" is meaningless without the role it took effect on.
type nodeUpdateResult struct {
	ID           string         `json:"id"`
	Hostname     string         `json:"hostname"`
	Version      uint64         `json:"version"`
	Section      string         `json:"section"`
	Role         string         `json:"role"`
	Availability string         `json:"availability"`
	Details      map[string]any `json:"details"`
}

// serviceUpdate projects the service a write returned down to the section that
// was edited.
func serviceUpdate(section string, svc swarm.Service) serviceUpdateResult {
	return serviceUpdateResult{
		ID:      svc.ID,
		Name:    svc.Spec.Name,
		Version: svc.Version.Index,
		Section: section,
		Details: selectKeys(cluster.ServiceDetails(svc), serviceSectionKeys[section]),
	}
}

// nodeUpdate is the node counterpart. It reads the section's detail out of the
// digest projection rather than off the spec directly, for the same reason
// serviceUpdate does: one description of a node, not two.
func nodeUpdate(section string, node swarm.Node) nodeUpdateResult {
	details := cluster.NodeDigest(node, nil, nil).Details

	return nodeUpdateResult{
		ID:           node.ID,
		Hostname:     node.Description.Hostname,
		Version:      node.Version.Index,
		Section:      section,
		Role:         stringDetail(details, "role"),
		Availability: stringDetail(details, "availability"),
		Details:      selectKeys(details, nodeSectionKeys[section]),
	}
}

// selectKeys copies the named keys that are present. The result is never nil:
// an empty object says "this section is set to nothing", which a null could
// not distinguish from "this tool forgot to answer".
func selectKeys(details map[string]any, keys []string) map[string]any {
	out := make(map[string]any, len(keys))

	for _, key := range keys {
		if value, ok := details[key]; ok {
			out[key] = value
		}
	}

	return out
}

func stringDetail(details map[string]any, key string) string {
	value, _ := details[key].(string)

	return value
}
