package cluster

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/cache"
)

// Row is one entry in a list of cluster resources. The raw Docker object is
// the wrong answer to a list question: it is mostly Platforms and PreviousSpec,
// and whether the thing is healthy is not in it at all, since state is derived
// from tasks. ID and Name are both always present.
type Row struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	// Type is the resource type, singular: "service", "node", "task", ...
	Type string `json:"type"`

	// Stack is the owning stack namespace, where the resource belongs to one.
	Stack string `json:"stack,omitempty"`

	// State is the derived condition — a service's DeriveServiceState, a node's
	// status — not a raw Docker enum.
	State string `json:"state,omitempty"`

	// Detail is the single most identifying secondary fact: the image for a
	// service, the role for a node, the driver for a network.
	Detail string `json:"detail,omitempty"`

	// Desired and Running are populated only for types where a replica count
	// means something, so a caller can tell "2 of 3" from "no such concept".
	Desired int `json:"desired,omitempty"`
	Running int `json:"running,omitempty"`
}

// RowsForServices builds the list view of services. State and running count
// are derived from tasks, not from the spec, so the counts come in — already
// aggregated by cache.RunningTaskCounts under one read lock, rather than
// cloning the whole task table here to reduce it to one integer per service.
func RowsForServices(services []swarm.Service, running map[string]int) []Row {
	rows := make([]Row, 0, len(services))

	for _, svc := range services {
		var image string
		if svc.Spec.TaskTemplate.ContainerSpec != nil {
			image = StripImageDigest(svc.Spec.TaskTemplate.ContainerSpec.Image)
		}

		rows = append(rows, Row{
			ID:      svc.ID,
			Name:    svc.Spec.Name,
			Type:    "service",
			Stack:   svc.Spec.Labels["com.docker.stack.namespace"],
			State:   DeriveServiceState(svc, running[svc.ID]),
			Detail:  image,
			Desired: ReplicaCount(svc),
			Running: running[svc.ID],
		})
	}

	sortRows(rows)

	return rows
}

// sortRows puts a list into a stable order. Callers build rows by ranging over
// cache slices whose order is not guaranteed, and the result is marshalled into
// an MCP result a client may cache by ETag.
func sortRows(rows []Row) {
	slices.SortFunc(rows, func(a, b Row) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}

		return strings.Compare(a.ID, b.ID)
	})
}

// RowsForNodes builds the list view of cluster nodes. Detail is the node's
// role, which is what distinguishes two otherwise identical ready nodes.
func RowsForNodes(nodes []swarm.Node) []Row {
	rows := make([]Row, 0, len(nodes))

	for _, n := range nodes {
		rows = append(rows, Row{
			ID:     n.ID,
			Name:   n.Description.Hostname,
			Type:   "node",
			State:  deriveNodeState(n),
			Detail: string(n.Spec.Role),
		})
	}

	sortRows(rows)

	return rows
}

// RowsForTasks builds the list view of tasks. Services name each task's
// parent, which its own record holds only as an ID; the node half comes off
// the enriched task, which resolved it already. The services slice and the
// enrichment must both be ACL-filtered, or a row names what describe withholds.
func RowsForTasks(tasks []EnrichedTask, services []swarm.Service) []Row {
	serviceByID := make(map[string]*swarm.Service, len(services))
	for i := range services {
		serviceByID[services[i].ID] = &services[i]
	}

	rows := make([]Row, 0, len(tasks))

	for _, task := range tasks {
		// The hostname when it is readable, the node ID otherwise — the same
		// fallback TaskDigest makes, and no disclosure either way, since the
		// task record the caller can already read carries the ID itself.
		where := task.NodeHostname
		if where == "" {
			where = task.NodeID
		}

		rows = append(rows, Row{
			ID:     task.ID,
			Name:   TaskName(task.Task, serviceByID[task.ServiceID]),
			Type:   "task",
			State:  string(task.Status.State),
			Detail: where,
		})
	}

	sortRows(rows)

	return rows
}

// RowsForConfigs builds the list view of configs. Config data is base64 and
// never belongs in a list, so Detail stays empty.
func RowsForConfigs(configs []swarm.Config) []Row {
	rows := make([]Row, 0, len(configs))

	for _, cfg := range configs {
		rows = append(rows, Row{
			ID:    cfg.ID,
			Name:  cfg.Spec.Name,
			Type:  "config",
			Stack: cfg.Spec.Labels["com.docker.stack.namespace"],
		})
	}

	sortRows(rows)

	return rows
}

// RowsForSecrets builds the list view of secrets. A secret's data is zeroed
// before it reaches here and nothing about it is ever placed in Detail.
func RowsForSecrets(secrets []swarm.Secret) []Row {
	rows := make([]Row, 0, len(secrets))

	for _, sec := range secrets {
		rows = append(rows, Row{
			ID:    sec.ID,
			Name:  sec.Spec.Name,
			Type:  "secret",
			Stack: sec.Spec.Labels["com.docker.stack.namespace"],
		})
	}

	sortRows(rows)

	return rows
}

// RowsForNetworks builds the list view of networks.
func RowsForNetworks(networks []network.Summary) []Row {
	rows := make([]Row, 0, len(networks))

	for _, net := range networks {
		rows = append(rows, Row{
			ID:     net.ID,
			Name:   net.Name,
			Type:   "network",
			Stack:  net.Labels["com.docker.stack.namespace"],
			Detail: net.Driver,
		})
	}

	sortRows(rows)

	return rows
}

// RowsForVolumes builds the list view of volumes. Volumes are keyed by Name
// rather than ID everywhere in Cetacean, so both fields carry the name.
func RowsForVolumes(volumes []volume.Volume) []Row {
	rows := make([]Row, 0, len(volumes))

	for _, vol := range volumes {
		rows = append(rows, Row{
			ID:     vol.Name,
			Name:   vol.Name,
			Type:   "volume",
			Stack:  vol.Labels["com.docker.stack.namespace"],
			Detail: vol.Driver,
		})
	}

	sortRows(rows)

	return rows
}

// RowsForStacks builds the list view of stacks. A stack is derived from labels
// rather than being a Docker primitive, so the count of services it holds is
// what makes it worth listing, and that is its Desired.
func RowsForStacks(stacks []cache.Stack) []Row {
	rows := make([]Row, 0, len(stacks))

	for _, st := range stacks {
		rows = append(rows, Row{
			ID:      st.Name,
			Name:    st.Name,
			Type:    "stack",
			Desired: len(st.Services),
		})
	}

	sortRows(rows)

	return rows
}

// Digest is the detail view of one resource: everything a caller needs to
// decide what to do, without a second question. Reason is what earns the type:
// every read reporting a non-healthy state also reports the cause Swarm gave.
type Digest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`

	State string `json:"state,omitempty"`

	// Reason explains a non-healthy State, and is empty when there is nothing
	// to explain.
	Reason string `json:"reason,omitempty"`

	// Since is when the current State began: the oldest failing task's
	// timestamp, captured before RecentFailures is capped, or the resource's
	// own last-updated time when no failure dates it — including a healthy
	// State, which a failure cannot explain however recent.
	Since string `json:"since,omitempty"`

	// Details is type-specific. It is deliberately open in the advertised
	// output schema: eight describe_<type> tools would tighten it at the cost
	// of eight entries in every tools/list, and the envelope is where the
	// tightness matters.
	Details map[string]any `json:"details,omitempty"`

	// Related are the resources this one references or is referenced by, so a
	// caller can traverse without a second search.
	Related []Related `json:"related"`

	// RecentFailures are the task failures behind the current State, newest
	// first, capped at maxRecentFailures.
	RecentFailures []TaskFailure `json:"recentFailures"`

	// Restarts counts involuntary task terminations behind a State that looks
	// fine. Nil when no tracker was consulted — a service that has never
	// restarted and one that was never measured are different answers, and a
	// confident zero would conflate them. Services only.
	Restarts *ServiceRestarts `json:"restarts,omitempty"`
}

// ServiceRestarts counts involuntary task terminations over a short window and
// a long one. Only the ratio separates a fault that started with this deploy
// from one that has run for a week. It is also the only channel carrying the
// *rate* of a restart loop: a crash-looping service is still "running".
type ServiceRestarts struct {
	LastHour uint64 `json:"lastHour"`
	LastWeek uint64 `json:"lastWeek"`

	// TrackingSince is the earliest moment the counts can account for. The
	// tracker starts with the process, so on a young one both figures are
	// bounded by this rather than by their labels and the ratio collapses.
	// Compare it against the window before concluding "new".
	TrackingSince string `json:"trackingSince,omitempty"`
}

// Related is one cross-reference from a Digest.
type Related struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`

	// Relation names the direction, e.g. "attached-to", "mounts", "runs-on".
	Relation string `json:"relation"`
}

// TaskFailure is one task that did not run, with the reason it did not.
type TaskFailure struct {
	TaskID  string `json:"taskId"`
	At      string `json:"at,omitempty"`
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

// maxRecentFailures bounds what a digest carries. A service restarting in a
// loop can hold dozens of failed records and they all say the same thing.
const maxRecentFailures = 5

// ServiceDigest builds the detail view of one service. networks names its
// attachments, which carry only an ID; an unmatched Target falls back to the
// ID, since the slice may be ACL-filtered. restarts is passed in, nil included,
// so this stays a pure function of the records handed to it.
func ServiceDigest(
	svc swarm.Service,
	tasks []swarm.Task,
	networks []network.Summary,
	restarts *ServiceRestarts,
) Digest {
	var (
		running    int
		oldestFail time.Time
		haveOldest bool
	)

	failures := make([]TaskFailure, 0, len(tasks))

	for _, task := range tasks {
		if task.ServiceID != svc.ID {
			continue
		}

		// Not Status.State alone: a task marked for shutdown keeps
		// Status.State: running while it drains, and RunningTaskCounts —
		// what every list row counts with — excludes those.
		if cache.CountsAsRunningReplica(task) {
			running++

			continue
		}

		// A task earns a place here only when Swarm calls it an involuntary
		// failure or gives an explicit cause for not running. Mid-startup
		// states are not failures, and neither are the cleanly shut-down
		// tasks a rolling update leaves behind for every replica it moved.
		if !cache.IsFailureState(task.Status.State) && task.Status.Err == "" {
			continue
		}

		// Replacement decides nothing for a failed task: a crash loop is made
		// entirely of replaced ones, since Swarm shuts a task down the instant
		// it fails. It still decides for a merely *unplaced* task — an
		// explicit cause with no failure state — where a replaced one is history.
		if !cache.IsFailureState(task.Status.State) && !TaskIsLive(task) {
			continue
		}

		// Captured before the cap below, so a service with more failures
		// than maxRecentFailures can still be dated back to its first one.
		if !haveOldest || task.Status.Timestamp.Before(oldestFail) {
			oldestFail = task.Status.Timestamp
			haveOldest = true
		}

		failures = append(failures, TaskFailure{
			TaskID:  task.ID,
			At:      task.Status.Timestamp.UTC().Format(time.RFC3339),
			State:   string(task.Status.State),
			Message: taskReason(task),
		})
	}

	slices.SortFunc(failures, func(a, b TaskFailure) int {
		if c := strings.Compare(b.At, a.At); c != 0 {
			return c
		}

		return strings.Compare(a.TaskID, b.TaskID)
	})

	if len(failures) > maxRecentFailures {
		failures = failures[:maxRecentFailures]
	}

	state := DeriveServiceState(svc, running)

	digest := Digest{
		ID:             svc.ID,
		Name:           svc.Spec.Name,
		Type:           "service",
		State:          state,
		Details:        ServiceDetails(svc),
		Related:        serviceRelated(svc, networks),
		RecentFailures: failures,
		Restarts:       restarts,
	}

	// Since dates the state above, so only a state a failure explains may be
	// dated from one. Swarm keeps a terminal record for every replica it has
	// replaced, and dating a "running" service from a fault that is over is
	// worse than not answering at all.
	if haveOldest && state != "running" {
		digest.Since = oldestFail.UTC().Format(time.RFC3339)
	} else {
		digest.Since = svc.UpdatedAt.UTC().Format(time.RFC3339)
	}

	if state != "running" {
		switch {
		case len(failures) > 0:
			digest.Reason = failures[0].Message
		case svc.UpdateStatus != nil && svc.UpdateStatus.Message != "":
			// A failing task is the more actionable explanation; the update
			// status is what is left to explain "updating" (and any other
			// non-running state) once no task failure accounts for it.
			digest.Reason = svc.UpdateStatus.Message
		}
	}

	return digest
}

// taskReason is the cause Swarm gave for a task not running. Status.Err holds
// the actionable text ("no suitable node (scheduling constraints not satisfied
// on 1 node)"); Status.Message is the lifecycle narration ("pending task
// scheduling") and is only worth returning when there is no error.
func taskReason(task swarm.Task) string {
	if task.Status.Err != "" {
		return task.Status.Err
	}

	return task.Status.Message
}

// serviceRelated builds the cross-references for a service's Digest: the
// networks it attaches to and the volumes it mounts. A bind or tmpfs mount
// names a host path rather than a Cetacean resource, so it has nothing to
// traverse to and is reported in Details instead.
func serviceRelated(svc swarm.Service, networks []network.Summary) []Related {
	networkNames := make(map[string]string, len(networks))
	for _, net := range networks {
		networkNames[net.ID] = net.Name
	}

	attachments := svc.Spec.TaskTemplate.Networks
	related := make([]Related, 0, len(attachments))

	for _, attachment := range attachments {
		name := networkNames[attachment.Target]
		if name == "" {
			// The caller may have filtered the networks slice by ACL; falling
			// back to the ID keeps Name non-empty rather than breaking the
			// rule every Row and Digest relies on.
			name = attachment.Target
		}

		related = append(related, Related{
			ID:       attachment.Target,
			Name:     name,
			Type:     "network",
			Relation: "attached-to",
		})
	}

	if spec := svc.Spec.TaskTemplate.ContainerSpec; spec != nil {
		for _, m := range spec.Mounts {
			if m.Type != mount.TypeVolume {
				continue
			}

			related = append(related, Related{
				ID:       m.Source,
				Name:     m.Source,
				Type:     "volume",
				Relation: "mounts",
			})
		}
	}

	sortRelated(related)

	return related
}

// sortRelated puts a Digest's cross-references into a stable order — the same
// rule sortRows applies to a Row list, and for the same reason: every caller
// builds this from a map or an unordered slice, and the result is marshalled
// into an MCP result a client may cache by ETag.
func sortRelated(related []Related) {
	slices.SortFunc(related, func(a, b Related) int {
		if c := strings.Compare(a.Type, b.Type); c != 0 {
			return c
		}

		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}

		return strings.Compare(a.ID, b.ID)
	})
}

// ServiceDetails is the type-specific body of a service's Digest. Every
// numeric field names its unit: Docker expresses CPU in NanoCPUs and durations
// in nanoseconds, which read as unlabelled large integers.
func ServiceDetails(svc swarm.Service) map[string]any {
	details := map[string]any{
		"mode":     serviceMode(svc),
		"replicas": ReplicaCount(svc),
	}

	spec := svc.Spec.TaskTemplate.ContainerSpec
	if spec != nil {
		details["image"] = StripImageDigest(spec.Image)

		// Docker's split, kept: Command is the entrypoint, Args is what
		// follows it. Reporting Args as "command" hides an entrypoint
		// override, and is a trap once a caller can write this section.
		if len(spec.Command) > 0 {
			details["command"] = spec.Command
		}

		if len(spec.Args) > 0 {
			details["args"] = spec.Args
		}

		names := make([]string, 0, len(spec.Env))
		for _, entry := range spec.Env {
			name, _, _ := strings.Cut(entry, "=")
			names = append(names, name)
		}

		slices.Sort(names)
		details["envNames"] = names

		// Durations as strings, never Docker's nanosecond integers: "10s" is
		// a value a caller can write back. Each key is omitted when unset, so
		// "not configured" reads as absence rather than as a configured zero.
		if hc := spec.Healthcheck; hc != nil {
			details["healthcheck"] = true

			if len(hc.Test) > 0 {
				details["healthcheckTest"] = hc.Test
			}

			if hc.Interval > 0 {
				details["healthcheckInterval"] = hc.Interval.String()
			}

			if hc.Timeout > 0 {
				details["healthcheckTimeout"] = hc.Timeout.String()
			}

			if hc.StartPeriod > 0 {
				details["healthcheckStartPeriod"] = hc.StartPeriod.String()
			}

			if hc.Retries > 0 {
				details["healthcheckRetries"] = hc.Retries
			}
		} else {
			details["healthcheck"] = false
		}

		// Names only, the rule envNames and the log driver's optionNames
		// already follow. Which secrets a container receives is exactly what
		// makes a rotation verifiable — "did the repoint land?" — while the
		// values behind them stay unreachable through every read.
		if refs := spec.Secrets; len(refs) > 0 {
			names := make([]string, 0, len(refs))
			for _, ref := range refs {
				names = append(names, ref.SecretName)
			}

			slices.Sort(names)
			details["secretNames"] = names
		}

		if refs := spec.Configs; len(refs) > 0 {
			names := make([]string, 0, len(refs))
			for _, ref := range refs {
				names = append(names, ref.ConfigName)
			}

			slices.Sort(names)
			details["configNames"] = names
		}

		// Targets first, over every mount: bindMounts below skips volumes, so
		// it cannot say whether a data volume survived a wholesale
		// replacement. A target is a mount's identity.
		if len(spec.Mounts) > 0 {
			targets := make([]string, 0, len(spec.Mounts))
			for _, m := range spec.Mounts {
				targets = append(targets, m.Target)
			}

			slices.Sort(targets)
			details["mountTargets"] = targets
		}

		var bindMounts []map[string]any

		for _, m := range spec.Mounts {
			if m.Type == mount.TypeVolume {
				continue
			}

			bindMounts = append(bindMounts, map[string]any{
				"type":     string(m.Type),
				"source":   m.Source,
				"target":   m.Target,
				"readOnly": m.ReadOnly,
			})
		}

		if len(bindMounts) > 0 {
			details["bindMounts"] = bindMounts
		}
	}

	if res := svc.Spec.TaskTemplate.Resources; res != nil {
		if res.Limits != nil {
			if res.Limits.NanoCPUs > 0 {
				details["cpuLimitCores"] = float64(res.Limits.NanoCPUs) / 1e9
			}

			if res.Limits.MemoryBytes > 0 {
				details["memoryLimitBytes"] = res.Limits.MemoryBytes
			}
		}

		if res.Reservations != nil {
			if res.Reservations.NanoCPUs > 0 {
				details["cpuReservationCores"] = float64(res.Reservations.NanoCPUs) / 1e9
			}

			if res.Reservations.MemoryBytes > 0 {
				details["memoryReservationBytes"] = res.Reservations.MemoryBytes
			}
		}
	}

	if p := svc.Spec.TaskTemplate.Placement; p != nil && len(p.Constraints) > 0 {
		details["placementConstraints"] = p.Constraints
	}

	if len(svc.Spec.Labels) > 0 {
		details["labels"] = svc.Spec.Labels
	}

	// Option *names* only, the rule envNames follows: a log driver's options
	// routinely carry a credential, such as a splunk-token or an
	// authenticated syslog address.
	if driver := svc.Spec.TaskTemplate.LogDriver; driver != nil && driver.Name != "" {
		logDriver := map[string]any{"name": driver.Name}

		if len(driver.Options) > 0 {
			names := slices.Sorted(maps.Keys(driver.Options))
			logDriver["optionNames"] = names
		}

		details["logDriver"] = logDriver
	}

	if endpoint := svc.Spec.EndpointSpec; endpoint != nil && len(endpoint.Ports) > 0 {
		ports := make([]map[string]any, 0, len(endpoint.Ports))

		for _, port := range endpoint.Ports {
			ports = append(ports, map[string]any{
				"published": port.PublishedPort,
				"target":    port.TargetPort,
				"protocol":  string(port.Protocol),
				"mode":      string(port.PublishMode),
			})
		}

		details["ports"] = ports
	}

	if policy := updatePolicyDetails(svc.Spec.UpdateConfig); policy != nil {
		details["updatePolicy"] = policy
	}

	if policy := updatePolicyDetails(svc.Spec.RollbackConfig); policy != nil {
		details["rollbackPolicy"] = policy
	}

	if svc.UpdateStatus != nil && svc.UpdateStatus.State != "" {
		details["updateState"] = string(svc.UpdateStatus.State)

		if svc.UpdateStatus.Message != "" {
			details["updateMessage"] = svc.UpdateStatus.Message
		}
	}

	return details
}

// updatePolicyDetails names an UpdateConfig's fields for the update or
// rollback policy in a service's Details. Delay and Monitor are
// time.Duration, i.e. unlabelled nanosecond integers in Docker's own type, so
// both are reported as duration strings instead.
func updatePolicyDetails(cfg *swarm.UpdateConfig) map[string]any {
	if cfg == nil {
		return nil
	}

	policy := map[string]any{
		"parallelism": cfg.Parallelism,
	}

	if cfg.Delay > 0 {
		policy["delay"] = cfg.Delay.String()
	}

	if cfg.Monitor > 0 {
		policy["monitor"] = cfg.Monitor.String()
	}

	if cfg.FailureAction != "" {
		policy["failureAction"] = cfg.FailureAction
	}

	if cfg.Order != "" {
		policy["order"] = cfg.Order
	}

	return policy
}

// serviceMode names a service's scheduling mode without exposing the nested
// Docker union a caller would otherwise have to interpret.
func serviceMode(svc swarm.Service) string {
	if svc.Spec.Mode.Global != nil {
		return "global"
	}

	return "replicated"
}
