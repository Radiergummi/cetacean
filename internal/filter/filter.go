package filter

import (
	"fmt"
	"strconv"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"cetacean/internal/cache"
)

// Program is a compiled filter expression.
type Program = *vm.Program

// Compile parses and compiles a filter expression string.
// Returns an error if the expression is syntactically invalid.
func Compile(expression string) (Program, error) {
	return expr.Compile(expression, expr.AsBool())
}

// Evaluate runs a compiled program against an environment map.
func Evaluate(prog Program, env map[string]any) (bool, error) {
	out, err := expr.Run(prog, env)
	if err != nil {
		return false, err
	}
	b, ok := out.(bool)
	if !ok {
		return false, fmt.Errorf("filter expression must return bool, got %T", out)
	}
	return b, nil
}

// NodeEnv builds an expression environment from a swarm.Node.
func NodeEnv(n swarm.Node) map[string]any {
	return map[string]any{
		"id":           n.ID,
		"name":         n.Description.Hostname,
		"state":        string(n.Status.State),
		"role":         string(n.Spec.Role),
		"availability": string(n.Spec.Availability),
	}
}

// ServiceEnv builds an expression environment from a swarm.Service.
func ServiceEnv(s swarm.Service) map[string]any {
	mode := "replicated"
	if s.Spec.Mode.Global != nil {
		mode = "global"
	}
	var image string
	if s.Spec.TaskTemplate.ContainerSpec != nil {
		image = s.Spec.TaskTemplate.ContainerSpec.Image
	}
	stack := s.Spec.Labels["com.docker.stack.namespace"]
	return map[string]any{
		"id":    s.ID,
		"name":  s.Spec.Name,
		"image": image,
		"mode":  mode,
		"stack": stack,
	}
}

// TaskEnv builds an expression environment from a swarm.Task.
func TaskEnv(t swarm.Task) map[string]any {
	var image string
	if t.Spec.ContainerSpec != nil {
		image = t.Spec.ContainerSpec.Image
	}
	var exitCode string
	if t.Status.ContainerStatus != nil {
		exitCode = strconv.Itoa(t.Status.ContainerStatus.ExitCode)
	}
	return map[string]any{
		"id":            t.ID,
		"state":         string(t.Status.State),
		"desired_state": string(t.DesiredState),
		"image":         image,
		"exit_code":     exitCode,
		"error":         t.Status.Err,
		"service":       t.ServiceID,
		"node":          t.NodeID,
		"slot":          t.Slot,
	}
}

// ConfigEnv builds an expression environment from a swarm.Config.
func ConfigEnv(c swarm.Config) map[string]any {
	return map[string]any{
		"id":   c.ID,
		"name": c.Spec.Name,
	}
}

// SecretEnv builds an expression environment from a swarm.Secret.
func SecretEnv(s swarm.Secret) map[string]any {
	return map[string]any{
		"id":   s.ID,
		"name": s.Spec.Name,
	}
}

// NetworkEnv builds an expression environment from a network.Summary.
func NetworkEnv(n network.Summary) map[string]any {
	return map[string]any{
		"id":     n.ID,
		"name":   n.Name,
		"driver": n.Driver,
		"scope":  n.Scope,
	}
}

// VolumeEnv builds an expression environment from a volume.Volume.
func VolumeEnv(v volume.Volume) map[string]any {
	return map[string]any{
		"name":   v.Name,
		"driver": v.Driver,
		"scope":  v.Scope,
	}
}

// StackEnv builds an expression environment from a cache.Stack.
func StackEnv(s cache.Stack) map[string]any {
	return map[string]any{
		"name":     s.Name,
		"services": len(s.Services),
		"configs":  len(s.Configs),
		"secrets":  len(s.Secrets),
		"networks": len(s.Networks),
		"volumes":  len(s.Volumes),
	}
}

// ResourceEnv builds an expression environment from an interface{} resource,
// as stored in cache.Event.Resource. Returns nil for unrecognized types.
func ResourceEnv(resource any) map[string]any {
	switch r := resource.(type) {
	case swarm.Node:
		return NodeEnv(r)
	case swarm.Service:
		return ServiceEnv(r)
	case swarm.Task:
		return TaskEnv(r)
	case swarm.Config:
		return ConfigEnv(r)
	case swarm.Secret:
		return SecretEnv(r)
	case network.Summary:
		return NetworkEnv(r)
	case volume.Volume:
		return VolumeEnv(r)
	default:
		return nil
	}
}
