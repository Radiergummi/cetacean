package projection

import (
	"fmt"
	"slices"
	"strings"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/quorum"
)

// Impact is what an impactful write will do, as a person confirming it is
// told. Every name in it is one the caller may read; what they may not is
// only counted, in HiddenDependents.
type Impact struct {
	Summary string   `json:"summary"`
	Effects []string `json:"effects,omitempty"`

	// Relation says how the dependents relate to the target: "Used by".
	Relation         string   `json:"relation,omitempty"`
	Dependents       []string `json:"dependents,omitempty"`
	HiddenDependents int      `json:"hiddenDependents,omitempty"`

	Irreversible string `json:"irreversible,omitempty"`
}

// Message renders the impact as the plain text of a confirmation prompt.
func (i Impact) Message() string {
	var b strings.Builder

	b.WriteString(i.Summary)

	for _, effect := range i.Effects {
		b.WriteString("\n- ")
		b.WriteString(effect)
	}

	if line := i.dependentsLine(); line != "" {
		b.WriteString("\n- ")
		b.WriteString(line)
	}

	if i.Irreversible != "" {
		b.WriteString("\n\n")
		b.WriteString(i.Irreversible)
	}

	return b.String()
}

func (i Impact) dependentsLine() string {
	if len(i.Dependents) == 0 && i.HiddenDependents == 0 {
		return ""
	}

	return i.Relation + " " + i.dependentList() + "."
}

// dependentList names the dependents and counts the ones the caller can't see.
func (i Impact) dependentList() string {
	named := len(i.Dependents)

	parts := make([]string, 0, 2)
	if named > 0 {
		parts = append(parts, strings.Join(i.Dependents, ", "))
	}

	if i.HiddenDependents > 0 {
		other := "other "
		if named == 0 {
			other = ""
		}

		parts = append(parts, plural(i.HiddenDependents, other+"service", other+"services")+
			" you can't see")
	}

	return strings.Join(parts, " and ")
}

// splitReadable names the services readable admits and counts the rest.
func splitReadable(names []string, readable func(resource string) bool) ([]string, int) {
	var (
		named  []string
		hidden int
	)

	for _, name := range names {
		if readable("service:" + name) {
			named = append(named, name)
		} else {
			hidden++
		}
	}

	slices.Sort(named)

	return named, hidden
}

// ServiceRemovalImpact describes removing svc, which runs running tasks now.
func ServiceRemovalImpact(svc swarm.Service, running int) Impact {
	return Impact{
		Summary: fmt.Sprintf("Remove service %s.", svc.Spec.Name),
		Effects: []string{
			fmt.Sprintf("Stops its %s.", plural(running, "running task", "running tasks")),
			"Keeps the volumes, networks, configs and secrets it references.",
		},
		Irreversible: "This can't be undone: the service's specification and history are deleted.",
	}
}

// TaskRemovalImpact describes removing task, named label, whose service is
// named service, or empty when the caller can't read it.
func TaskRemovalImpact(label, service string) Impact {
	effect := "Swarm starts a replacement if the task's service still wants one."
	if service != "" {
		effect = fmt.Sprintf("Swarm starts a replacement for service %s.", service)
	}

	return Impact{
		Summary: fmt.Sprintf("Remove task %s.", label),
		Effects: []string{"Stops the task's container.", effect},
	}
}

// irreversibleRemovals is what removing each referenced type loses for good.
var irreversibleRemovals = map[string]string{
	"config":  "This can't be undone: the config's content can't be recovered.",
	"secret":  "This can't be undone: the secret's value can't be recovered.",
	"network": "This can't be undone: services must be updated to attach to a new network.",
	"volume":  "This can't be undone: the volume's contents are deleted.",
}

// ReferenceRemovalImpact describes removing a config, secret, network or
// volume named name, which users reference. Docker refuses only while a
// current spec uses it; one only a previous spec names is a rollback target
// the removal breaks. Only the users readable admits are named.
func ReferenceRemovalImpact(
	resourceType, name string,
	users []cache.ServiceRef,
	readable func(resource string) bool,
) Impact {
	var current, rollback []string
	for _, user := range users {
		if user.Rollback {
			rollback = append(rollback, user.Name)
		} else {
			current = append(current, user.Name)
		}
	}

	named, hidden := splitReadable(current, readable)

	impact := Impact{
		Summary:          fmt.Sprintf("Remove %s %s.", resourceType, name),
		Relation:         "Used by",
		Dependents:       named,
		HiddenDependents: hidden,
		Irreversible:     irreversibleRemovals[resourceType],
	}

	if len(current) > 0 {
		impact.Effects = append(impact.Effects,
			"Docker refuses the removal while a service still uses it.")
	}

	if len(rollback) > 0 {
		rollbackNamed, rollbackHidden := splitReadable(rollback, readable)
		impact.Effects = append(impact.Effects, "Removing it breaks a rollback of "+
			Impact{Dependents: rollbackNamed, HiddenDependents: rollbackHidden}.dependentList()+".")
	}

	return impact
}

// DrainImpact describes draining node from graph, its DrainImpactGraph over
// what the caller may read; hidden counts the services on the node they may not.
func DrainImpact(node swarm.Node, graph cluster.TopologyGraph, hidden int) Impact {
	var movable, global, stranded []string

	for _, vertex := range graph.Nodes {
		if vertex.Type != "service" {
			continue
		}

		switch vertex.State {
		case cluster.DrainStateMovable:
			movable = append(movable, vertex.Label)
		case cluster.DrainStateGlobal:
			global = append(global, vertex.Label)
		case cluster.DrainStateStranded:
			stranded = append(stranded, vertex.Label)
		}
	}

	for _, names := range [][]string{movable, global, stranded} {
		slices.Sort(names)
	}

	impact := Impact{
		Summary:          fmt.Sprintf("Drain node %s.", cluster.NodeACLName(node)),
		Relation:         "Runs tasks of",
		Dependents:       slices.Concat(movable, global, stranded),
		HiddenDependents: hidden,
		Effects:          []string{"Stops scheduling new tasks on it."},
		Irreversible:     "Making the node active again doesn't move tasks back to it.",
	}

	if len(movable) > 0 {
		impact.Effects = append(impact.Effects, fmt.Sprintf(
			"Moves the tasks of %s to other nodes.",
			plural(len(movable), "service", "services")))
	}

	if len(global) > 0 {
		impact.Effects = append(impact.Effects, fmt.Sprintf(
			"Stops the tasks of %s here: %s.",
			plural(len(global), "global service", "global services"),
			strings.Join(global, ", ")))
	}

	if len(stranded) > 0 {
		impact.Effects = append(impact.Effects, fmt.Sprintf(
			"Leaves %s with nowhere to run: %s.",
			plural(len(stranded), "service", "services"),
			strings.Join(stranded, ", ")))
	}

	if graph.Note != "" {
		impact.Effects = append(impact.Effects, "This was "+graph.Note+".")
	}

	return impact
}

// AvailabilityImpact describes setting node to pause or active. Drain has
// DrainImpact, since only it moves work.
func AvailabilityImpact(node swarm.Node, availability swarm.NodeAvailability) Impact {
	name := cluster.NodeACLName(node)

	if availability == swarm.NodeAvailabilityPause {
		return Impact{
			Summary: fmt.Sprintf("Pause node %s.", name),
			Effects: []string{"Stops scheduling new tasks on it; running tasks stay."},
		}
	}

	return Impact{
		Summary: fmt.Sprintf("Make node %s active.", name),
		Effects: []string{"Lets Swarm schedule new tasks on it again."},
	}
}

// RoleImpact describes changing node's role, with the manager count and the
// quorum before and after counted over every node in the cluster.
func RoleImpact(node swarm.Node, nodes []swarm.Node, role swarm.NodeRole) Impact {
	name := cluster.NodeACLName(node)

	if node.Spec.Role == role {
		return Impact{Summary: fmt.Sprintf("Node %s is already a %s; nothing changes.", name, role)}
	}

	q := quorum.Of(slices.Values(nodes))
	managers, reachable := q.Managers, q.Reachable

	after, reachableAfter := managers+1, reachable+1
	summary := fmt.Sprintf("Promote node %s to manager.", name)

	if role == swarm.NodeRoleWorker {
		after, reachableAfter = managers-1, reachable
		if quorum.Reachable(node) {
			reachableAfter--
		}

		summary = fmt.Sprintf("Demote node %s to worker.", name)
	}

	effects := []string{
		fmt.Sprintf("Managers: %d before, %d after.", managers, after),
	}

	needed := quorum.Size(after)

	switch {
	case after < 1:
		effects = append(effects, "Leaves the swarm with no manager.")
	case reachableAfter >= needed:
		effects = append(effects, fmt.Sprintf(
			"Quorum survives: %d of %d managers reachable, %d needed; it then tolerates %s.",
			reachableAfter, after, needed,
			plural(reachableAfter-needed, "manager failure", "manager failures")))
	default:
		effects = append(effects, fmt.Sprintf(
			"Quorum is lost: %d of %d managers reachable, %d needed.",
			reachableAfter, after, needed))
	}

	return Impact{Summary: summary, Effects: effects}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}

	return fmt.Sprintf("%d %s", n, many)
}
