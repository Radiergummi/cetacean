package api

import (
	"net/http"

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
		writeErrorCode(w, r, "API004", "stack not found")
		return
	}

	file, warnings := compose.FromStack(detail, h.cache.ListNetworks())
	h.writeCompose(w, r, file, warnings)
}

// HandleServiceCompose renders one service as a one-service document.
func (h *Handlers) HandleServiceCompose(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}
	h.setAllow(w, r, "service", svc.Spec.Name)

	file, warnings := compose.FromService(svc, h.cache.ListNetworks())
	h.writeCompose(w, r, file, warnings)
}

// writeCompose renders the document both handlers built the same way.
func (h *Handlers) writeCompose(
	w http.ResponseWriter,
	r *http.Request,
	file compose.File,
	warnings []string,
) {
	body, err := compose.Render(file, warnings)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "could not render compose document")
		return
	}

	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	writeRawWithETag(w, r, body)
}
