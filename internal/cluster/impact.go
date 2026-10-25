package cluster

import (
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/swarm"
)

// Impact is what an impactful write will do, as a person confirming it is
// told. Every name in it is one the caller may read; what they may not is
// only counted, in HiddenDependents.
type Impact struct {
	Summary          string   `json:"summary"`
	Effects          []string `json:"effects,omitempty"`
	Dependents       []string `json:"dependents,omitempty"`
	HiddenDependents int      `json:"hiddenDependents,omitempty"`
	Irreversible     string   `json:"irreversible,omitempty"`
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
	named := len(i.Dependents)
	if named == 0 && i.HiddenDependents == 0 {
		return ""
	}

	parts := make([]string, 0, 2)
	if named > 0 {
		parts = append(parts, strings.Join(i.Dependents, ", "))
	}

	if i.HiddenDependents > 0 {
		parts = append(parts, fmt.Sprintf(
			"%s you can't see",
			plural(i.HiddenDependents, "other service", "other services"),
		))
	}

	return "Used by " + strings.Join(parts, " and ") + "."
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

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}

	return fmt.Sprintf("%d %s", n, many)
}
