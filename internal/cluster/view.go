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

// Row is one entry in a list of cluster resources.
//
// It exists because the raw Docker object is the wrong answer to a list
// question: eight services of raw swarm.Service cost roughly fourteen thousand
// tokens, most of it Platforms entries and a duplicated PreviousSpec, and the
// field a caller actually asked for — whether the thing is healthy — is not in
// there at all, because state is derived from tasks.
//
// The shape is TopologyNode's, which already proved sufficient to diagnose a
// cluster in practice. Both ID and Name are always present so a caller never
// has to resolve one into the other, following the TargetID/TargetName pair the
// recommendations already use.
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

// RowsForServices builds the list view of services. The running counts are
// needed because a service's state and running count are derived from its
// tasks, not from its spec.
//
// It takes the counts rather than the tasks themselves so the caller can
// aggregate them wherever they already live — cache.RunningTaskCounts does it
// under one read lock — instead of cloning the whole task table to reduce it
// to one integer per service here.
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
			State:  DeriveNodeState(n),
			Detail: string(n.Spec.Role),
		})
	}

	sortRows(rows)

	return rows
}

// RowsForTasks builds the list view of tasks. Services are needed to name each
// task's parent: a task's own record holds only its ID, and a caller reading a
// task list is asking which service is broken and where.
//
// The node half of that answer comes off the enriched task rather than a second
// slice, because EnrichTasksWithin has already resolved it — against the same
// ACL-filtered node listing this would have taken — and re-deriving it made
// every find and every completion keystroke clone, sort and filter the whole
// node table a second time to recover a string it was handed.
//
// The services slice must already be filtered to what the caller may read, the
// way TaskDigest's is, and the tasks must have been enriched from an equally
// filtered node listing: a parent absent from either falls back to the ID the
// task record carries anyway, so a row never names a resource from behind the
// caller's grants. Passing the unfiltered cache listings made find disclose a
// service name that describe withheld for the very same task.
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
func RowsForVolumes(volumes []*volume.Volume) []Row {
	rows := make([]Row, 0, len(volumes))

	for _, vol := range volumes {
		if vol == nil {
			continue
		}

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
// decide what to do, and nothing they would have to ask a second question for.
//
// Reason is the field that earns the type. A state alone — "failed" — is not an
// answer to "why is this broken", so every read that reports a non-healthy
// state also reports the cause Swarm gave for it, and a follow-up call is not
// required to learn it.
type Digest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`

	State string `json:"state,omitempty"`

	// Reason explains a non-healthy State, and is empty when there is nothing
	// to explain.
	Reason string `json:"reason,omitempty"`

	// Since is when the current State began: the oldest still-live failing
	// task's timestamp, captured before RecentFailures is capped to the
	// newest few, or the resource's own last-updated time when there is no
	// failure to date it from. Answers "how long has this been going on"
	// without a second call into history.
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

// ServiceRestarts counts a service's involuntary task terminations over a
// short window and a long one.
//
// One number cannot answer the question an operator is actually asking. A
// service failing twenty times in the last hour is either a fault that started
// during this deploy or one that has run for a week, and only the ratio of the
// two windows tells them apart — so the digest carries both rather than a rate
// the reader would have to trust blindly.
//
// This is the only channel through which a restart loop reaches a digest.
// DeriveServiceState reports "running" whenever a replica is up, and
// ServiceDigest drops tasks the orchestrator has already replaced, so a
// service crash-looping every four seconds otherwise describes as healthy.
type ServiceRestarts struct {
	LastHour uint64 `json:"lastHour"`
	LastWeek uint64 `json:"lastWeek"`
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

// ServiceDigest builds the detail view of one service. networks resolves the
// names of its network attachments, which carry only an ID; the caller may
// have already filtered the slice by ACL, so a Target with no match falls
// back to the ID rather than leaving Related.Name empty.
//
// restarts is the caller's reading of the restart tracker, or nil when it has
// none to give; it is passed in rather than looked up so this stays a pure
// function of the records handed to it, as every other builder here is.
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

		if task.Status.State == swarm.TaskStateRunning {
			running++

			continue
		}

		// A task the orchestrator has already replaced explains history, not
		// the current state, and including it would attribute an old failure
		// to a service that has since recovered.
		if !taskIsLive(task) {
			continue
		}

		// Ordinary mid-startup or mid-rollout states — preparing, starting,
		// assigned, new — are not failures; DeriveServiceState already
		// reports "pending" for those without help. A task only earns a
		// place here when Swarm itself calls it an involuntary failure or
		// gives an explicit cause for not running.
		if !cache.IsFailureState(task.Status.State) && task.Status.Err == "" {
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

	if haveOldest {
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

// ServiceDetails is the type-specific body of a service's Digest.
//
// Every numeric field names its unit. Docker's own types express CPU in
// NanoCPUs and durations in nanoseconds, which read as unlabelled large
// integers and have already caused one shipped bug; a caller should not have to
// know that 10000000000 is ten seconds.
func ServiceDetails(svc swarm.Service) map[string]any {
	details := map[string]any{
		"mode":     serviceMode(svc),
		"replicas": ReplicaCount(svc),
	}

	spec := svc.Spec.TaskTemplate.ContainerSpec
	if spec != nil {
		details["image"] = StripImageDigest(spec.Image)

		// Docker's split, kept: Command is the entrypoint, Args is what
		// follows it. This used to report Args under the name "command" and
		// never reported Command at all, which hid an entrypoint override —
		// and once a caller can write this section, writing Command and
		// reading Args back under the same name is a trap.
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

		// Durations as strings, never the nanosecond integers Docker's own
		// type carries: "10s" is a value a caller can read and write back,
		// 10000000000 is one they have to decode. Each key is omitted when
		// unset, so "not configured" reads as absence rather than as a zero
		// a caller might mistake for a configured value.
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

		// Targets first, over every mount: bindMounts below reports the
		// security-relevant ones in full but skips volumes, so it cannot on
		// its own answer whether a data volume survived a wholesale
		// replacement. A target is a mount's identity — two things cannot be
		// mounted at one path.
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

	// Option *names* only, the same rule envNames follows: a log driver's
	// options routinely carry a credential (`splunk-token`, an authenticated
	// syslog address), and a digest that named the driver honestly while
	// handing over its token would be a worse leak than the one envNames
	// exists to prevent.
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
