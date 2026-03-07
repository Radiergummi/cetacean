package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	h.serveLogs(w, r, func(ctx context.Context, tail string, follow bool, since, until string) (LogStream, error) {
		return h.dockerClient.ServiceLogs(ctx, id, tail, follow, since, until)
	})
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
