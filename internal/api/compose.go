package api

import (
	"net/http"

	"github.com/docker/docker/api/types/network"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/compose"
)

// HandleStackCompose renders a stack as a compose document. It reads the stack
// through the same lookup and the same detail call the JSON handler uses: a
// second derivation of membership is how an export becomes a way around the
// grant that gates the JSON.
func (h *Handlers) HandleStackCompose(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, ok := lookupACL(
		h, w, r, "stack", name,
		h.cache.GetStack,
		func(s cache.Stack) string { return "stack:" + name },
	); !ok {
		return
	}
	h.setAllow(w, r, "stack", name)

	detail, ok := h.cache.GetStackDetail(name)
	if !ok {
		writeErrorCode(w, r, "STK001", "stack not found")
		return
	}

	file, warnings := compose.FromStack(detail, h.readableNetworks(r))
	h.writeCompose(w, r, file, warnings)
}

// HandleServiceCompose renders one service as a one-service document.
func (h *Handlers) HandleServiceCompose(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}
	h.setAllow(w, r, "service", svc.Spec.Name)

	file, warnings := compose.FromService(svc, h.readableNetworks(r))
	h.writeCompose(w, r, file, warnings)
}

// readableNetworks is the list the projection resolves attachment IDs against.
// It is filtered, because resolving an ID the caller may not read would put
// that network's name in a document their grant does not cover; the projection
// leaves an unresolved attachment as its ID and says so in the header.
func (h *Handlers) readableNetworks(r *http.Request) []network.Summary {
	return acl.Filter(
		h.acl,
		auth.IdentityFromContext(r.Context()),
		"read",
		h.cache.ListNetworks(),
		func(n network.Summary) string { return "network:" + n.Name },
	)
}

// writeCompose renders the document both handlers built the same way.
func (h *Handlers) writeCompose(
	w http.ResponseWriter,
	r *http.Request,
	file compose.File,
	warnings []string,
) {
	// A service with no ContainerSpec — a plugin, or a network attachment —
	// is not a compose service, and the projection emits nothing for it. An
	// empty document is not a smaller answer, it is one no loader accepts.
	if len(file.Services) == 0 {
		writeErrorCode(w, r, "API014", "this resource has nothing a compose file can describe")
		return
	}

	body, err := compose.Render(file, warnings)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "could not render compose document")
		return
	}

	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	writeRawWithETag(w, r, body)
}
