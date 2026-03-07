package api

import (
	"encoding/json"
	"net/http"

	"cetacean/internal/cache"
)

type Handlers struct {
	cache *cache.Cache
}

func NewHandlers(c *cache.Cache) *Handlers {
	return &Handlers{cache: c}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (h *Handlers) HandleCluster(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cache.Snapshot())
}

// --- Nodes ---

func (h *Handlers) HandleListNodes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cache.ListNodes())
}

func (h *Handlers) HandleGetNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, ok := h.cache.GetNode(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, node)
}

// --- Services ---

func (h *Handlers) HandleListServices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cache.ListServices())
}

func (h *Handlers) HandleGetService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, svc)
}

// --- Tasks ---

func (h *Handlers) HandleListTasks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cache.ListTasks())
}

func (h *Handlers) HandleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, ok := h.cache.GetTask(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, task)
}

// --- Stacks ---

func (h *Handlers) HandleListStacks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cache.ListStacks())
}

func (h *Handlers) HandleGetStack(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	stack, ok := h.cache.GetStack(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, stack)
}

// --- Configs ---

func (h *Handlers) HandleListConfigs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cache.ListConfigs())
}

// --- Secrets ---

func (h *Handlers) HandleListSecrets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cache.ListSecrets())
}

// --- Networks ---

func (h *Handlers) HandleListNetworks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cache.ListNetworks())
}

// --- Volumes ---

func (h *Handlers) HandleListVolumes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cache.ListVolumes())
}
