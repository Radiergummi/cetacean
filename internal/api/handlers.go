package api

import (
	"context"
	json "github.com/goccy/go-json"
	"io"
	"net/http"
	"strings"

	"github.com/docker/docker/pkg/stdcopy"

	"cetacean/internal/cache"
)

type DockerLogStreamer interface {
	ServiceLogs(ctx context.Context, serviceID string, tail string) (io.ReadCloser, error)
}

type Handlers struct {
	cache        *cache.Cache
	dockerClient DockerLogStreamer
}

func NewHandlers(c *cache.Cache, dc DockerLogStreamer) *Handlers {
	return &Handlers{cache: c, dockerClient: dc}
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
	nodes := h.cache.ListNodes()
	if q := r.URL.Query().Get("search"); q != "" {
		q = strings.ToLower(q)
		filtered := nodes[:0]
		for _, n := range nodes {
			if strings.Contains(strings.ToLower(n.Description.Hostname), q) {
				filtered = append(filtered, n)
			}
		}
		nodes = filtered
	}
	writeJSON(w, nodes)
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

func (h *Handlers) HandleNodeTasks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := h.cache.GetNode(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, h.cache.ListTasksByNode(id))
}

// --- Services ---

func (h *Handlers) HandleListServices(w http.ResponseWriter, r *http.Request) {
	services := h.cache.ListServices()
	if q := r.URL.Query().Get("search"); q != "" {
		q = strings.ToLower(q)
		filtered := services[:0]
		for _, s := range services {
			if strings.Contains(strings.ToLower(s.Spec.Name), q) {
				filtered = append(filtered, s)
			}
		}
		services = filtered
	}
	writeJSON(w, services)
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

func (h *Handlers) HandleServiceTasks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := h.cache.GetService(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, h.cache.ListTasksByService(id))
}

func (h *Handlers) HandleServiceLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := h.cache.GetService(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = "200"
	}

	logs, err := h.dockerClient.ServiceLogs(r.Context(), id, tail)
	if err != nil {
		http.Error(w, "failed to get logs", http.StatusInternalServerError)
		return
	}
	defer logs.Close() //nolint:errcheck // best-effort close

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = stdcopy.StdCopy(w, w, logs)
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
	stacks := h.cache.ListStacks()
	if q := r.URL.Query().Get("search"); q != "" {
		q = strings.ToLower(q)
		filtered := stacks[:0]
		for _, s := range stacks {
			if strings.Contains(strings.ToLower(s.Name), q) {
				filtered = append(filtered, s)
			}
		}
		stacks = filtered
	}
	writeJSON(w, stacks)
}

func (h *Handlers) HandleGetStack(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	detail, ok := h.cache.GetStackDetail(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, detail)
}

// --- Configs ---

func (h *Handlers) HandleListConfigs(w http.ResponseWriter, r *http.Request) {
	configs := h.cache.ListConfigs()
	if q := r.URL.Query().Get("search"); q != "" {
		q = strings.ToLower(q)
		filtered := configs[:0]
		for _, c := range configs {
			if strings.Contains(strings.ToLower(c.Spec.Name), q) {
				filtered = append(filtered, c)
			}
		}
		configs = filtered
	}
	writeJSON(w, configs)
}

// --- Secrets ---

func (h *Handlers) HandleListSecrets(w http.ResponseWriter, r *http.Request) {
	secrets := h.cache.ListSecrets()
	if q := r.URL.Query().Get("search"); q != "" {
		q = strings.ToLower(q)
		filtered := secrets[:0]
		for _, s := range secrets {
			if strings.Contains(strings.ToLower(s.Spec.Name), q) {
				filtered = append(filtered, s)
			}
		}
		secrets = filtered
	}
	writeJSON(w, secrets)
}

// --- Networks ---

func (h *Handlers) HandleListNetworks(w http.ResponseWriter, r *http.Request) {
	networks := h.cache.ListNetworks()
	if q := r.URL.Query().Get("search"); q != "" {
		q = strings.ToLower(q)
		filtered := networks[:0]
		for _, n := range networks {
			if strings.Contains(strings.ToLower(n.Name), q) {
				filtered = append(filtered, n)
			}
		}
		networks = filtered
	}
	writeJSON(w, networks)
}

// --- Volumes ---

func (h *Handlers) HandleListVolumes(w http.ResponseWriter, r *http.Request) {
	volumes := h.cache.ListVolumes()
	if q := r.URL.Query().Get("search"); q != "" {
		q = strings.ToLower(q)
		filtered := volumes[:0]
		for _, v := range volumes {
			if strings.Contains(strings.ToLower(v.Name), q) {
				filtered = append(filtered, v)
			}
		}
		volumes = filtered
	}
	writeJSON(w, volumes)
}
