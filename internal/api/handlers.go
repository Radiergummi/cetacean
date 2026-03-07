package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	json "github.com/goccy/go-json"

	"cetacean/internal/cache"
)

const defaultLogLimit = 500
const maxLogLimit = 10000

type DockerLogStreamer interface {
	ServiceLogs(ctx context.Context, serviceID string, tail string, follow bool, since, until string) (io.ReadCloser, error)
	TaskLogs(ctx context.Context, taskID string, tail string, follow bool, since, until string) (io.ReadCloser, error)
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
	_ = json.NewEncoder(w).Encode(v)
}

func searchFilter[T any](items []T, query string, name func(T) string) []T {
	if query == "" {
		return items
	}
	q := strings.ToLower(query)
	filtered := items[:0]
	for _, item := range items {
		if strings.Contains(strings.ToLower(name(item)), q) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (h *Handlers) HandleCluster(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cache.Snapshot())
}

// --- Nodes ---

func (h *Handlers) HandleListNodes(w http.ResponseWriter, r *http.Request) {
	nodes := h.cache.ListNodes()
	nodes = searchFilter(nodes, r.URL.Query().Get("search"), func(n swarm.Node) string { return n.Description.Hostname })
	p := parsePagination(r)
	nodes = sortItems(nodes, p.Sort, p.Dir, map[string]func(swarm.Node) string{
		"hostname":     func(n swarm.Node) string { return n.Description.Hostname },
		"role":         func(n swarm.Node) string { return string(n.Spec.Role) },
		"status":       func(n swarm.Node) string { return string(n.Status.State) },
		"availability": func(n swarm.Node) string { return string(n.Spec.Availability) },
	})
	writeJSON(w, applyPagination(nodes, p))
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
	services = searchFilter(services, r.URL.Query().Get("search"), func(s swarm.Service) string { return s.Spec.Name })
	p := parsePagination(r)
	services = sortItems(services, p.Sort, p.Dir, map[string]func(swarm.Service) string{
		"name": func(s swarm.Service) string { return s.Spec.Name },
		"mode": func(s swarm.Service) string {
			if s.Spec.Mode.Global != nil {
				return "Global"
			}
			return "Replicated"
		},
	})
	writeJSON(w, applyPagination(services, p))
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
	h.serveLogs(w, r, func(ctx context.Context, tail string, follow bool, since, until string) (LogStream, error) {
		return h.dockerClient.ServiceLogs(ctx, id, tail, follow, since, until)
	})
}

// --- Tasks ---

func (h *Handlers) HandleListTasks(w http.ResponseWriter, r *http.Request) {
	tasks := h.cache.ListTasks()
	p := parsePagination(r)
	tasks = sortItems(tasks, p.Sort, p.Dir, map[string]func(swarm.Task) string{
		"state":   func(t swarm.Task) string { return string(t.Status.State) },
		"service": func(t swarm.Task) string { return t.ServiceID },
		"node":    func(t swarm.Task) string { return t.NodeID },
	})
	writeJSON(w, applyPagination(tasks, p))
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

func (h *Handlers) HandleTaskLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := h.cache.GetTask(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.serveLogs(w, r, func(ctx context.Context, tail string, follow bool, since, until string) (LogStream, error) {
		return h.dockerClient.TaskLogs(ctx, id, tail, follow, since, until)
	})
}

// LogStream is an alias for the io.ReadCloser returned by Docker log APIs.
type LogStream = io.ReadCloser

// LogResponse is the JSON response for paginated log fetches.
type LogResponse struct {
	Lines  []LogLine `json:"lines"`
	Oldest string    `json:"oldest"`
	Newest string    `json:"newest"`
}

type logFetcher func(ctx context.Context, tail string, follow bool, since, until string) (LogStream, error)

func wantsSSE(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

func (h *Handlers) serveLogs(w http.ResponseWriter, r *http.Request, fetch logFetcher) {
	q := r.URL.Query()
	since := q.Get("after")
	until := q.Get("before")
	streamFilter := q.Get("stream") // "", "stdout", or "stderr"

	if wantsSSE(r) {
		h.serveLogsSSE(w, r, fetch, since, streamFilter)
		return
	}

	limit := defaultLogLimit
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLogLimit {
		limit = maxLogLimit
	}

	logs, err := fetch(r.Context(), strconv.Itoa(limit), false, since, until)
	if err != nil {
		http.Error(w, "failed to get logs", http.StatusInternalServerError)
		return
	}
	defer logs.Close() //nolint:errcheck

	lines, err := ParseDockerLogs(logs)
	if err != nil {
		http.Error(w, "failed to parse logs", http.StatusInternalServerError)
		return
	}
	if lines == nil {
		lines = []LogLine{}
	}

	if streamFilter != "" {
		filtered := lines[:0]
		for _, l := range lines {
			if l.Stream == streamFilter {
				filtered = append(filtered, l)
			}
		}
		lines = filtered
	}

	resp := LogResponse{Lines: lines}
	if len(lines) > 0 {
		resp.Oldest = lines[0].Timestamp
		resp.Newest = lines[len(lines)-1].Timestamp
	}
	writeJSON(w, resp)
}

func (h *Handlers) serveLogsSSE(w http.ResponseWriter, r *http.Request, fetch logFetcher, since, streamFilter string) {
	// EventSource sends Last-Event-ID on reconnect; use it as fallback for since
	if since == "" {
		since = r.Header.Get("Last-Event-ID")
	}
	logs, err := fetch(r.Context(), "0", true, since, "")
	if err != nil {
		http.Error(w, "failed to get logs", http.StatusInternalServerError)
		return
	}
	defer logs.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ch := make(chan LogLine, 64)
	done := make(chan error, 1)
	go func() {
		done <- StreamDockerLogs(logs, ch)
		close(ch)
	}()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case line, ok := <-ch:
			if !ok {
				<-done
				return
			}
			if streamFilter != "" && line.Stream != streamFilter {
				continue
			}
			data, _ := json.Marshal(line)
			if line.Timestamp != "" {
				fmt.Fprintf(w, "id: %s\n", line.Timestamp)
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			<-done
			return
		}
	}
}

// --- Stacks ---

func (h *Handlers) HandleListStacks(w http.ResponseWriter, r *http.Request) {
	stacks := h.cache.ListStacks()
	stacks = searchFilter(stacks, r.URL.Query().Get("search"), func(s cache.Stack) string { return s.Name })
	p := parsePagination(r)
	stacks = sortItems(stacks, p.Sort, p.Dir, map[string]func(cache.Stack) string{
		"name": func(s cache.Stack) string { return s.Name },
	})
	writeJSON(w, applyPagination(stacks, p))
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
	configs = searchFilter(configs, r.URL.Query().Get("search"), func(c swarm.Config) string { return c.Spec.Name })
	p := parsePagination(r)
	configs = sortItems(configs, p.Sort, p.Dir, map[string]func(swarm.Config) string{
		"name": func(c swarm.Config) string { return c.Spec.Name },
	})
	writeJSON(w, applyPagination(configs, p))
}

// --- Secrets ---

func (h *Handlers) HandleListSecrets(w http.ResponseWriter, r *http.Request) {
	secrets := h.cache.ListSecrets()
	secrets = searchFilter(secrets, r.URL.Query().Get("search"), func(s swarm.Secret) string { return s.Spec.Name })
	p := parsePagination(r)
	secrets = sortItems(secrets, p.Sort, p.Dir, map[string]func(swarm.Secret) string{
		"name": func(s swarm.Secret) string { return s.Spec.Name },
	})
	writeJSON(w, applyPagination(secrets, p))
}

// --- Networks ---

func (h *Handlers) HandleListNetworks(w http.ResponseWriter, r *http.Request) {
	networks := h.cache.ListNetworks()
	networks = searchFilter(networks, r.URL.Query().Get("search"), func(n network.Summary) string { return n.Name })
	p := parsePagination(r)
	networks = sortItems(networks, p.Sort, p.Dir, map[string]func(network.Summary) string{
		"name":   func(n network.Summary) string { return n.Name },
		"driver": func(n network.Summary) string { return n.Driver },
	})
	writeJSON(w, applyPagination(networks, p))
}

// --- Volumes ---

func (h *Handlers) HandleListVolumes(w http.ResponseWriter, r *http.Request) {
	volumes := h.cache.ListVolumes()
	volumes = searchFilter(volumes, r.URL.Query().Get("search"), func(v volume.Volume) string { return v.Name })
	p := parsePagination(r)
	volumes = sortItems(volumes, p.Sort, p.Dir, map[string]func(volume.Volume) string{
		"name":   func(v volume.Volume) string { return v.Name },
		"driver": func(v volume.Volume) string { return v.Driver },
	})
	writeJSON(w, applyPagination(volumes, p))
}
