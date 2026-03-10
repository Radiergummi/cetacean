package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	json "github.com/goccy/go-json"

	"cetacean/internal/cache"
	"cetacean/internal/docker"
	"cetacean/internal/filter"
	"cetacean/internal/notify"
	"cetacean/internal/version"
)

const defaultLogLimit = 500
const maxLogLimit = 10000
const maxLogSSEConns = 128

var activeLogSSEConns atomic.Int64

type DockerLogStreamer interface {
	Logs(ctx context.Context, kind docker.LogKind, id string, tail string, follow bool, since, until string) (io.ReadCloser, error)
}

type Handlers struct {
	cache        *cache.Cache
	dockerClient DockerLogStreamer
	ready        <-chan struct{}
	notifier     *notify.Notifier
	promClient   *PromClient
}

func NewHandlers(c *cache.Cache, dc DockerLogStreamer, ready <-chan struct{}, notifier *notify.Notifier, promClient *PromClient) *Handlers {
	return &Handlers{cache: c, dockerClient: dc, ready: ready, notifier: notifier, promClient: promClient}
}

func (h *Handlers) isReady() bool {
	select {
	case <-h.ready:
		return true
	default:
		return false
	}
}

func (h *Handlers) HandleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{
		"status":    "ok",
		"version":   version.Version,
		"commit":    version.Commit,
		"buildDate": version.Date,
	})
}

func (h *Handlers) HandleReady(w http.ResponseWriter, r *http.Request) {
	if !h.isReady() {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"status": "not_ready"})
		return
	}
	writeJSON(w, map[string]string{"status": "ready"})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":  msg,
		"status": status,
	})
}

func searchFilter[T any](items []T, query string, name func(T) string) []T {
	if query == "" {
		return items
	}
	q := strings.ToLower(query)
	var filtered []T
	for _, item := range items {
		if strings.Contains(strings.ToLower(name(item)), q) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

const maxFilterLen = 512

func exprFilter[T any](items []T, expr string, env func(T) map[string]any, w http.ResponseWriter) ([]T, bool) {
	if expr == "" {
		return items, true
	}
	if len(expr) > maxFilterLen {
		writeError(w, http.StatusBadRequest, "filter expression too long")
		return nil, false
	}
	prog, err := filter.Compile(expr)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid filter expression: %s", err))
		return nil, false
	}
	var filtered []T
	for _, item := range items {
		ok, err := filter.Evaluate(prog, env(item))
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("filter evaluation error: %s", err))
			return nil, false
		}
		if ok {
			filtered = append(filtered, item)
		}
	}
	return filtered, true
}

func (h *Handlers) HandleCluster(w http.ResponseWriter, r *http.Request) {
	snap := h.cache.Snapshot()
	writeJSON(w, struct {
		cache.ClusterSnapshot
		PrometheusConfigured bool `json:"prometheusConfigured"`
	}{snap, h.promClient != nil})
}

// --- Nodes ---

func (h *Handlers) HandleListNodes(w http.ResponseWriter, r *http.Request) {
	nodes := h.cache.ListNodes()
	nodes = searchFilter(nodes, r.URL.Query().Get("search"), func(n swarm.Node) string { return n.Description.Hostname })
	var ok bool
	if nodes, ok = exprFilter(nodes, r.URL.Query().Get("filter"), filter.NodeEnv, w); !ok {
		return
	}
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
		writeError(w, http.StatusNotFound, fmt.Sprintf("node %q not found", id))
		return
	}
	writeJSON(w, node)
}

func (h *Handlers) HandleNodeTasks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := h.cache.GetNode(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("node %q not found", id))
		return
	}
	writeJSON(w, h.cache.ListTasksByNode(id))
}

// --- Services ---

type ServiceListItem struct {
	swarm.Service
	RunningTasks int `json:"RunningTasks"`
}

func (h *Handlers) HandleListServices(w http.ResponseWriter, r *http.Request) {
	services := h.cache.ListServices()
	services = searchFilter(services, r.URL.Query().Get("search"), func(s swarm.Service) string { return s.Spec.Name })
	var ok bool
	if services, ok = exprFilter(services, r.URL.Query().Get("filter"), filter.ServiceEnv, w); !ok {
		return
	}
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
	paged := applyPagination(services, p)

	items := make([]ServiceListItem, len(paged.Items))
	for i, svc := range paged.Items {
		items[i] = ServiceListItem{
			Service:      svc,
			RunningTasks: h.cache.RunningTaskCount(svc.ID),
		}
	}

	writeJSON(w, PagedResponse[ServiceListItem]{Items: items, Total: paged.Total})
}

func (h *Handlers) HandleGetService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("service %q not found", id))
		return
	}
	writeJSON(w, svc)
}

func (h *Handlers) HandleServiceTasks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := h.cache.GetService(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("service %q not found", id))
		return
	}
	writeJSON(w, h.cache.ListTasksByService(id))
}

func (h *Handlers) HandleServiceLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := h.cache.GetService(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("service %q not found", id))
		return
	}
	h.serveLogs(w, r, func(ctx context.Context, tail string, follow bool, since, until string) (LogStream, error) {
		return h.dockerClient.Logs(ctx, docker.ServiceLog, id, tail, follow, since, until)
	})
}

// --- Tasks ---

func (h *Handlers) HandleListTasks(w http.ResponseWriter, r *http.Request) {
	tasks := h.cache.ListTasks()
	var ok bool
	if tasks, ok = exprFilter(tasks, r.URL.Query().Get("filter"), filter.TaskEnv, w); !ok {
		return
	}
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
		writeError(w, http.StatusNotFound, fmt.Sprintf("task %q not found", id))
		return
	}
	writeJSON(w, task)
}

func (h *Handlers) HandleTaskLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := h.cache.GetTask(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("task %q not found", id))
		return
	}
	h.serveLogs(w, r, func(ctx context.Context, tail string, follow bool, since, until string) (LogStream, error) {
		return h.dockerClient.Logs(ctx, docker.TaskLog, id, tail, follow, since, until)
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

func validLogTimestamp(s string) bool {
	if s == "" {
		return true
	}
	if _, err := time.Parse(time.RFC3339, s); err == nil {
		return true
	}
	if _, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return true
	}
	if _, err := time.ParseDuration(s); err == nil {
		return true
	}
	return false
}

func (h *Handlers) serveLogs(w http.ResponseWriter, r *http.Request, fetch logFetcher) {
	q := r.URL.Query()
	since := q.Get("after")
	until := q.Get("before")
	streamFilter := q.Get("stream") // "", "stdout", or "stderr"

	if !validLogTimestamp(since) {
		writeError(w, http.StatusBadRequest, `invalid "after" parameter: must be RFC3339 timestamp or Go duration`)
		return
	}
	if !validLogTimestamp(until) {
		writeError(w, http.StatusBadRequest, `invalid "before" parameter: must be RFC3339 timestamp or Go duration`)
		return
	}

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

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	logs, err := fetch(ctx, strconv.Itoa(limit), false, since, until)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get logs: %s", err))
		return
	}
	defer logs.Close() //nolint:errcheck

	lines, err := ParseDockerLogs(logs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to parse logs: %s", err))
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
	if activeLogSSEConns.Load() >= maxLogSSEConns {
		writeError(w, http.StatusServiceUnavailable, "too many active log streams")
		return
	}
	activeLogSSEConns.Add(1)
	defer activeLogSSEConns.Add(-1)

	// EventSource sends Last-Event-ID on reconnect; use it as fallback for since
	if since == "" {
		since = r.Header.Get("Last-Event-ID")
	}
	logs, err := fetch(r.Context(), "0", true, since, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to stream logs: %s", err))
		return
	}
	defer logs.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
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
			logs.Close() // unblocks StreamDockerLogs's io.Read
			for range ch {
			}
			<-done
			return
		}
	}
}

// --- History ---

func (h *Handlers) HandleHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	entries := h.cache.History().List(cache.HistoryQuery{
		Type:       q.Get("type"),
		ResourceID: q.Get("resourceId"),
		Limit:      limit,
	})
	if entries == nil {
		entries = []cache.HistoryEntry{}
	}
	writeJSON(w, entries)
}

// --- Stacks ---

func (h *Handlers) HandleListStacks(w http.ResponseWriter, r *http.Request) {
	stacks := h.cache.ListStacks()
	stacks = searchFilter(stacks, r.URL.Query().Get("search"), func(s cache.Stack) string { return s.Name })
	var ok bool
	if stacks, ok = exprFilter(stacks, r.URL.Query().Get("filter"), filter.StackEnv, w); !ok {
		return
	}
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
		writeError(w, http.StatusNotFound, fmt.Sprintf("stack %q not found", name))
		return
	}
	writeJSON(w, detail)
}

const stackNamespaceLabel = "container_label_com_docker_stack_namespace"

func (h *Handlers) HandleStackSummary(w http.ResponseWriter, r *http.Request) {
	summaries := h.cache.ListStackSummaries()

	if h.promClient != nil && len(summaries) > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		var memByStack, cpuByStack map[string]float64
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			memByStack = h.queryStackMetric(ctx,
				`sum by (`+stackNamespaceLabel+`)(container_memory_usage_bytes)`)
		}()
		go func() {
			defer wg.Done()
			cpuByStack = h.queryStackMetric(ctx,
				`sum by (`+stackNamespaceLabel+`)(rate(container_cpu_usage_seconds_total[5m])) * 100`)
		}()
		wg.Wait()

		for i := range summaries {
			summaries[i].MemoryUsageBytes = int64(memByStack[summaries[i].Name])
			summaries[i].CPUUsagePercent = cpuByStack[summaries[i].Name]
		}
	}

	if summaries == nil {
		summaries = []cache.StackSummary{}
	}
	writeJSON(w, summaries)
}

func (h *Handlers) queryStackMetric(ctx context.Context, query string) map[string]float64 {
	results, err := h.promClient.InstantQuery(ctx, query)
	if err != nil {
		slog.Warn("prometheus stack metric query failed", "error", err)
		return nil
	}
	out := make(map[string]float64, len(results))
	for _, r := range results {
		if name := r.Labels[stackNamespaceLabel]; name != "" {
			out[name] = r.Value
		}
	}
	return out
}

// --- Configs ---

func (h *Handlers) HandleGetConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg, ok := h.cache.GetConfig(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("config %q not found", id))
		return
	}
	writeJSON(w, map[string]any{
		"config":   cfg,
		"services": h.cache.ServicesUsingConfig(id),
	})
}

func (h *Handlers) HandleListConfigs(w http.ResponseWriter, r *http.Request) {
	configs := h.cache.ListConfigs()
	configs = searchFilter(configs, r.URL.Query().Get("search"), func(c swarm.Config) string { return c.Spec.Name })
	var ok bool
	if configs, ok = exprFilter(configs, r.URL.Query().Get("filter"), filter.ConfigEnv, w); !ok {
		return
	}
	p := parsePagination(r)
	configs = sortItems(configs, p.Sort, p.Dir, map[string]func(swarm.Config) string{
		"name":    func(c swarm.Config) string { return c.Spec.Name },
		"created": func(c swarm.Config) string { return c.CreatedAt.String() },
		"updated": func(c swarm.Config) string { return c.UpdatedAt.String() },
	})
	writeJSON(w, applyPagination(configs, p))
}

// --- Secrets ---

func (h *Handlers) HandleGetSecret(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sec, ok := h.cache.GetSecret(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("secret %q not found", id))
		return
	}
	// Never expose secret data — clear it before responding.
	sec.Spec.Data = nil
	writeJSON(w, map[string]any{
		"secret":   sec,
		"services": h.cache.ServicesUsingSecret(id),
	})
}

func (h *Handlers) HandleListSecrets(w http.ResponseWriter, r *http.Request) {
	secrets := h.cache.ListSecrets()
	for i := range secrets {
		secrets[i].Spec.Data = nil
	}
	secrets = searchFilter(secrets, r.URL.Query().Get("search"), func(s swarm.Secret) string { return s.Spec.Name })
	var ok bool
	if secrets, ok = exprFilter(secrets, r.URL.Query().Get("filter"), filter.SecretEnv, w); !ok {
		return
	}
	p := parsePagination(r)
	secrets = sortItems(secrets, p.Sort, p.Dir, map[string]func(swarm.Secret) string{
		"name":    func(s swarm.Secret) string { return s.Spec.Name },
		"created": func(s swarm.Secret) string { return s.CreatedAt.String() },
		"updated": func(s swarm.Secret) string { return s.UpdatedAt.String() },
	})
	writeJSON(w, applyPagination(secrets, p))
}

// --- Networks ---

func (h *Handlers) HandleGetNetwork(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	net, ok := h.cache.GetNetwork(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("network %q not found", id))
		return
	}
	writeJSON(w, map[string]any{
		"network":  net,
		"services": h.cache.ServicesUsingNetwork(id),
	})
}

func (h *Handlers) HandleListNetworks(w http.ResponseWriter, r *http.Request) {
	networks := h.cache.ListNetworks()
	networks = searchFilter(networks, r.URL.Query().Get("search"), func(n network.Summary) string { return n.Name })
	var ok bool
	if networks, ok = exprFilter(networks, r.URL.Query().Get("filter"), filter.NetworkEnv, w); !ok {
		return
	}
	p := parsePagination(r)
	networks = sortItems(networks, p.Sort, p.Dir, map[string]func(network.Summary) string{
		"name":   func(n network.Summary) string { return n.Name },
		"driver": func(n network.Summary) string { return n.Driver },
		"scope":  func(n network.Summary) string { return n.Scope },
	})
	writeJSON(w, applyPagination(networks, p))
}

// --- Volumes ---

func (h *Handlers) HandleGetVolume(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	vol, ok := h.cache.GetVolume(name)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("volume %q not found", name))
		return
	}
	writeJSON(w, map[string]any{
		"volume":   vol,
		"services": h.cache.ServicesUsingVolume(name),
	})
}

func (h *Handlers) HandleListVolumes(w http.ResponseWriter, r *http.Request) {
	volumes := h.cache.ListVolumes()
	volumes = searchFilter(volumes, r.URL.Query().Get("search"), func(v volume.Volume) string { return v.Name })
	var ok bool
	if volumes, ok = exprFilter(volumes, r.URL.Query().Get("filter"), filter.VolumeEnv, w); !ok {
		return
	}
	p := parsePagination(r)
	volumes = sortItems(volumes, p.Sort, p.Dir, map[string]func(volume.Volume) string{
		"name":   func(v volume.Volume) string { return v.Name },
		"driver": func(v volume.Volume) string { return v.Driver },
		"scope":  func(v volume.Volume) string { return v.Scope },
	})
	writeJSON(w, applyPagination(volumes, p))
}

// --- Notifications ---

func (h *Handlers) HandleNotificationRules(w http.ResponseWriter, r *http.Request) {
	if h.notifier == nil {
		writeJSON(w, []struct{}{})
		return
	}
	writeJSON(w, h.notifier.RuleStatuses())
}

// --- Search ---

type searchResult struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
	State  string `json:"state,omitempty"`
}

func labelsMatch(labels map[string]string, q string) bool {
	for k, v := range labels {
		if strings.Contains(strings.ToLower(k), q) || strings.Contains(strings.ToLower(v), q) {
			return true
		}
	}
	return false
}

func (h *Handlers) HandleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "missing required query parameter: q")
		return
	}
	ql := strings.ToLower(q)

	limit := 3
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			limit = n
		}
	}

	type typeResults struct {
		key     string
		results []searchResult
		count   int
	}

	// Build service name lookup for tasks
	services := h.cache.ListServices()
	svcNames := make(map[string]string, len(services))
	for _, s := range services {
		svcNames[s.ID] = s.Spec.Name
	}

	var sections []typeResults

	// Services
	{
		var matches []searchResult
		count := 0
		for _, s := range services {
			hit := strings.Contains(strings.ToLower(s.Spec.Name), ql)
			if !hit && s.Spec.TaskTemplate.ContainerSpec != nil {
				hit = strings.Contains(strings.ToLower(s.Spec.TaskTemplate.ContainerSpec.Image), ql)
			}
			if !hit {
				hit = labelsMatch(s.Spec.Labels, ql)
			}
			if hit {
				count++
				if limit == 0 || len(matches) < limit {
					detail := ""
					if s.Spec.TaskTemplate.ContainerSpec != nil {
						detail = s.Spec.TaskTemplate.ContainerSpec.Image
						if i := strings.Index(detail, "@sha256:"); i > 0 {
							detail = detail[:i]
						}
					}
					running := h.cache.RunningTaskCount(s.ID)
				desired := 0
				if s.Spec.Mode.Replicated != nil && s.Spec.Mode.Replicated.Replicas != nil {
					desired = int(*s.Spec.Mode.Replicated.Replicas)
				} else if s.Spec.Mode.Global != nil {
					desired = -1 // global: just check running > 0
				}
				state := "running"
				if s.UpdateStatus != nil && s.UpdateStatus.State == swarm.UpdateStateUpdating {
					state = "updating"
				} else if desired == -1 {
					if running == 0 {
						state = "pending"
					}
				} else if desired > 0 && running == 0 {
					state = "failed"
				} else if running < desired {
					state = "pending"
				}
				matches = append(matches, searchResult{ID: s.ID, Name: s.Spec.Name, Detail: detail, State: state})
				}
			}
		}
		if count > 0 {
			sections = append(sections, typeResults{"services", matches, count})
		}
	}

	// Stacks
	{
		stacks := h.cache.ListStacks()
		var matches []searchResult
		count := 0
		for _, s := range stacks {
			if strings.Contains(strings.ToLower(s.Name), ql) {
				count++
				if limit == 0 || len(matches) < limit {
					matches = append(matches, searchResult{
						ID:     s.Name,
						Name:   s.Name,
						Detail: fmt.Sprintf("%d services", len(s.Services)),
					})
				}
			}
		}
		if count > 0 {
			sections = append(sections, typeResults{"stacks", matches, count})
		}
	}

	// Nodes
	{
		nodes := h.cache.ListNodes()
		var matches []searchResult
		count := 0
		for _, n := range nodes {
			hit := strings.Contains(strings.ToLower(n.Description.Hostname), ql)
			if !hit {
				hit = strings.Contains(strings.ToLower(n.Status.Addr), ql)
			}
			if !hit {
				hit = labelsMatch(n.Spec.Labels, ql)
			}
			if hit {
				count++
				if limit == 0 || len(matches) < limit {
					matches = append(matches, searchResult{
						ID:     n.ID,
						Name:   n.Description.Hostname,
						Detail: fmt.Sprintf("%s, %s", n.Spec.Role, n.Status.State),
					})
				}
			}
		}
		if count > 0 {
			sections = append(sections, typeResults{"nodes", matches, count})
		}
	}

	// Tasks
	{
		tasks := h.cache.ListTasks()
		var matches []searchResult
		count := 0
		for _, t := range tasks {
			svcName := svcNames[t.ServiceID]
			taskName := fmt.Sprintf("%s.%d", svcName, t.Slot)

			hit := strings.Contains(strings.ToLower(svcName), ql)
			if !hit && t.Spec.ContainerSpec != nil {
				hit = strings.Contains(strings.ToLower(t.Spec.ContainerSpec.Image), ql)
			}
			if !hit && t.Spec.ContainerSpec != nil {
				hit = labelsMatch(t.Spec.ContainerSpec.Labels, ql)
			}
			if hit {
				count++
				if limit == 0 || len(matches) < limit {
					detail := ""
					if t.Spec.ContainerSpec != nil {
						detail = t.Spec.ContainerSpec.Image
						if i := strings.Index(detail, "@sha256:"); i > 0 {
							detail = detail[:i]
						}
					}
					matches = append(matches, searchResult{
						ID:     t.ID,
						Name:   taskName,
						Detail: detail,
						State:  string(t.Status.State),
					})
				}
			}
		}
		if count > 0 {
			sections = append(sections, typeResults{"tasks", matches, count})
		}
	}

	// Configs
	{
		configs := h.cache.ListConfigs()
		var matches []searchResult
		count := 0
		for _, c := range configs {
			hit := strings.Contains(strings.ToLower(c.Spec.Name), ql)
			if !hit {
				hit = labelsMatch(c.Spec.Labels, ql)
			}
			if hit {
				count++
				if limit == 0 || len(matches) < limit {
					matches = append(matches, searchResult{
						ID:     c.ID,
						Name:   c.Spec.Name,
						Detail: c.CreatedAt.Format(time.RFC3339),
					})
				}
			}
		}
		if count > 0 {
			sections = append(sections, typeResults{"configs", matches, count})
		}
	}

	// Secrets
	{
		secrets := h.cache.ListSecrets()
		var matches []searchResult
		count := 0
		for _, s := range secrets {
			s.Spec.Data = nil
			hit := strings.Contains(strings.ToLower(s.Spec.Name), ql)
			if !hit {
				hit = labelsMatch(s.Spec.Labels, ql)
			}
			if hit {
				count++
				if limit == 0 || len(matches) < limit {
					matches = append(matches, searchResult{
						ID:     s.ID,
						Name:   s.Spec.Name,
						Detail: s.CreatedAt.Format(time.RFC3339),
					})
				}
			}
		}
		if count > 0 {
			sections = append(sections, typeResults{"secrets", matches, count})
		}
	}

	// Networks
	{
		networks := h.cache.ListNetworks()
		var matches []searchResult
		count := 0
		for _, n := range networks {
			hit := strings.Contains(strings.ToLower(n.Name), ql)
			if !hit {
				hit = labelsMatch(n.Labels, ql)
			}
			if hit {
				count++
				if limit == 0 || len(matches) < limit {
					matches = append(matches, searchResult{
						ID:     n.ID,
						Name:   n.Name,
						Detail: n.Driver,
					})
				}
			}
		}
		if count > 0 {
			sections = append(sections, typeResults{"networks", matches, count})
		}
	}

	// Volumes
	{
		volumes := h.cache.ListVolumes()
		var matches []searchResult
		count := 0
		for _, v := range volumes {
			hit := strings.Contains(strings.ToLower(v.Name), ql)
			if !hit {
				hit = labelsMatch(v.Labels, ql)
			}
			if hit {
				count++
				if limit == 0 || len(matches) < limit {
					matches = append(matches, searchResult{
						ID:     v.Name,
						Name:   v.Name,
						Detail: v.Driver,
					})
				}
			}
		}
		if count > 0 {
			sections = append(sections, typeResults{"volumes", matches, count})
		}
	}

	results := make(map[string][]searchResult, len(sections))
	counts := make(map[string]int, len(sections))
	total := 0
	for _, s := range sections {
		results[s.key] = s.results
		counts[s.key] = s.count
		total += s.count
	}

	writeJSON(w, map[string]any{
		"query":   q,
		"results": results,
		"counts":  counts,
		"total":   total,
	})
}
