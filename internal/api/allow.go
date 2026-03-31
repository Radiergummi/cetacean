package api

import (
	"net/http"
	"strings"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"
)

// resourceMethods defines what write methods exist for each resource type
// at each operations tier.
type methodSpec struct {
	method string
	tier   config.OperationsLevel
}

// resourceWriteMethods maps resource types to their write methods and the
// minimum operations tier required. For methods that appear at multiple tiers
// (e.g. service PUT at tier1 for scale/image and tier3 for mode/endpoint-mode),
// the lowest tier is used so the method appears in Allow whenever any of its
// uses are enabled.
var resourceWriteMethods = map[string][]methodSpec{
	"service": {
		{"PUT", config.OpsOperational},     // scale, image (tier1); mode, endpoint-mode are tier3 but PUT is available if tier1 is enabled
		{"POST", config.OpsOperational},    // rollback, restart
		{"PATCH", config.OpsConfiguration}, // env, labels, resources, etc.
		{"DELETE", config.OpsImpactful},    // remove
	},
	"node": {
		{"PUT", config.OpsImpactful},    // availability, role
		{"PATCH", config.OpsImpactful},  // labels
		{"DELETE", config.OpsImpactful}, // remove
	},
	"task": {
		{"DELETE", config.OpsImpactful},
	},
	"config": {
		{"POST", config.OpsConfiguration},  // create
		{"PATCH", config.OpsConfiguration}, // labels
		{"DELETE", config.OpsImpactful},
	},
	"secret": {
		{"POST", config.OpsConfiguration},
		{"PATCH", config.OpsConfiguration},
		{"DELETE", config.OpsImpactful},
	},
	"network": {
		{"DELETE", config.OpsImpactful},
	},
	"volume": {
		{"DELETE", config.OpsImpactful},
	},
	"stack": {
		{"DELETE", config.OpsImpactful},
	},
	"plugin": {
		{"POST", config.OpsConfiguration},   // enable, disable (tier2); install, privileges, upgrade (tier3)
		{"PATCH", config.OpsConfiguration},  // settings
		{"DELETE", config.OpsImpactful},     // remove
	},
	"swarm": {
		{"PATCH", config.OpsConfiguration},  // orchestration, raft, dispatcher (tier2); ca, encryption (tier3)
		{"POST", config.OpsImpactful},       // rotate-token, rotate-unlock-key, force-rotate-ca, unlock
	},
}

// setAllow sets the Allow response header for a detail endpoint based on the
// configured operations level and ACL write permission.
func (h *Handlers) setAllow(w http.ResponseWriter, r *http.Request, resourceType, resourceName string) {
	methods := []string{"GET", "HEAD"}

	id := auth.IdentityFromContext(r.Context())
	canWrite := h.acl.Can(id, "write", resourceType+":"+resourceName)

	for _, spec := range resourceWriteMethods[resourceType] {
		if h.operationsLevel >= spec.tier && canWrite {
			methods = append(methods, spec.method)
		}
	}

	w.Header().Set("Allow", strings.Join(methods, ", "))
}

// setAllowList sets the Allow header for list endpoints.
func (h *Handlers) setAllowList(w http.ResponseWriter) {
	w.Header().Set("Allow", "GET, HEAD")
}
