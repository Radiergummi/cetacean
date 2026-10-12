package filter

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp/syntax"
	"strconv"
	"sync"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/vm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// Program is a compiled filter expression.
type Program = *vm.Program

// Compile parses and compiles a filter expression string.
// Returns an error if the expression is syntactically invalid.
func Compile(expression string) (Program, error) {
	if prog, ok := compileCache.get(expression); ok {
		return prog, nil
	}
	prog, err := expr.Compile(
		expression,
		expr.AsBool(),
		expr.Function(budgetCall, checkBudget),
		expr.Function(budgetSize, checkSize),
		expr.Function(budgetMatch, checkMatch),
		expr.Patch(budgetPatcher{}),
	)
	if err != nil {
		return nil, err
	}
	compileCache.put(expression, prog)
	return prog, nil
}

const compileCacheSize = 64

var compileCache = newProgramCache(compileCacheSize)

type programCache struct {
	mu      sync.RWMutex
	entries map[string]Program
	cap     int
}

func newProgramCache(cap int) *programCache {
	return &programCache{entries: make(map[string]Program, cap), cap: cap}
}

func (c *programCache) get(key string) (Program, bool) {
	c.mu.RLock()
	p, ok := c.entries[key]
	c.mu.RUnlock()
	return p, ok
}

func (c *programCache) put(key string, prog Program) {
	c.mu.Lock()
	if len(c.entries) >= c.cap {
		// Evict one random entry.
		for k := range c.entries {
			delete(c.entries, k)
			break
		}
	}
	c.entries[key] = prog
	c.mu.Unlock()
}

// ErrBudget reports that an evaluation outlived its context or that one step
// exceeded its size bound.
var ErrBudget = errors.New("filter evaluation budget exhausted")

// None of these names is a token the expression grammar can produce, so an
// expression can neither call a check itself nor shadow the context it reads.
const (
	budgetCall    = "budget check"
	budgetSize    = "budget size"
	budgetMatch   = "budget match"
	budgetContext = "budget context"
)

// A concatenation or a regexp match is one VM step whose cost grows with its
// operands, so each is bounded by size rather than by the deadline.
const (
	maxStringLen = 1 << 20
	maxMatchCost = 1 << 20 // subject bytes × pattern instructions
)

// budgetPatcher routes every predicate body through checkBudget, since every
// loop in the language iterates through a predicate, and bounds each `+`
// result and `matches` subject: expr bounds a run's memory but not its steps.
type budgetPatcher struct{}

func (budgetPatcher) Visit(node *ast.Node) {
	switch n := (*node).(type) {
	case *ast.PredicateNode:
		n.Node = &ast.SequenceNode{Nodes: []ast.Node{budgetNode(budgetCall), n.Node}}
	case *ast.BinaryNode:
		switch n.Operator {
		case "+":
			// A sum known to be numeric keeps its type for the checker.
			if k := n.Type().Kind(); k == reflect.String || k == reflect.Interface {
				*node = budgetNode(budgetSize, n)
			}
		case "matches":
			size := n.Right
			if s, ok := n.Right.(*ast.StringNode); ok {
				size = &ast.IntegerNode{Value: patternSize(s.Value)}
			}
			n.Left = budgetNode(budgetMatch, n.Left, size)
		}
	}
}

func budgetNode(name string, args ...ast.Node) *ast.CallNode {
	return &ast.CallNode{
		Callee:    &ast.IdentifierNode{Value: name},
		Arguments: append([]ast.Node{&ast.IdentifierNode{Value: budgetContext}}, args...),
	}
}

func checkBudget(params ...any) (any, error) {
	ctx, ok := params[0].(context.Context)
	if !ok || ctx.Err() != nil {
		return nil, ErrBudget
	}
	return nil, nil
}

func checkSize(params ...any) (any, error) {
	if s, ok := params[1].(string); ok && len(s) > maxStringLen {
		return nil, ErrBudget
	}
	return params[1], nil
}

// checkMatch passes the subject through. Its size argument is the pattern's
// instruction count, or the pattern itself when that is not a constant.
func checkMatch(params ...any) (any, error) {
	if _, err := checkBudget(params...); err != nil {
		return nil, err
	}
	size, _ := params[2].(int)
	if pattern, ok := params[2].(string); ok {
		size = patternSize(pattern)
	}
	if s, ok := params[1].(string); ok && len(s)*size > maxMatchCost {
		return nil, ErrBudget
	}
	return params[1], nil
}

// patternSize is the length of the program regexp runs for pattern, or 0 when
// pattern does not compile and the match reports that itself.
func patternSize(pattern string) int {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return 0
	}
	prog, err := syntax.Compile(re.Simplify())
	if err != nil {
		return 0
	}
	return len(prog.Inst)
}

// Evaluate runs a compiled program against an environment map.
func Evaluate(prog Program, env map[string]any) (bool, error) {
	return EvaluateContext(context.Background(), prog, env)
}

// EvaluateContext is Evaluate that stops with ErrBudget once ctx is done,
// including between iterations of the expression's own loops.
func EvaluateContext(ctx context.Context, prog Program, env map[string]any) (bool, error) {
	if ctx.Err() != nil {
		return false, ErrBudget
	}
	env[budgetContext] = ctx
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
func NodeEnv(n swarm.Node, m map[string]any) map[string]any {
	if m == nil {
		m = make(map[string]any, 5)
	}
	m["id"] = n.ID
	m["name"] = n.Description.Hostname
	m["state"] = boxedOr(nodeStates, n.Status.State)
	m["role"] = boxedOr(nodeRoles, n.Spec.Role)
	m["availability"] = boxedOr(nodeAvailabilities, n.Spec.Availability)
	return m
}

// ServiceEnv builds an expression environment from a swarm.Service.
func ServiceEnv(s swarm.Service, m map[string]any) map[string]any {
	if m == nil {
		m = make(map[string]any, 5)
	}
	mode := modeReplicated
	if s.Spec.Mode.Global != nil {
		mode = modeGlobal
	}
	image := emptyString
	if s.Spec.TaskTemplate.ContainerSpec != nil {
		image = s.Spec.TaskTemplate.ContainerSpec.Image
	}
	m["id"] = s.ID
	m["name"] = s.Spec.Name
	m["image"] = image
	m["mode"] = mode
	m["stack"] = s.Spec.Labels["com.docker.stack.namespace"]
	return m
}

// TaskEnv builds an expression environment from a swarm.Task.
func TaskEnv(t swarm.Task, m map[string]any) map[string]any {
	if m == nil {
		m = make(map[string]any, 9)
	}
	image := emptyString
	if t.Spec.ContainerSpec != nil {
		image = t.Spec.ContainerSpec.Image
	}
	// ExitCode is meaningless until the task reaches a terminal state — Docker
	// often reports -1 mid-run. Suppressing it here keeps filters like
	// `exit_code != "0"` from matching every running task.
	exitCode := emptyString
	if t.Status.ContainerStatus != nil && cache.IsTerminalState(t.Status.State) {
		exitCode = strconv.Itoa(t.Status.ContainerStatus.ExitCode)
	}
	m["id"] = t.ID
	m["state"] = boxedOr(taskStates, t.Status.State)
	m["desired_state"] = boxedOr(taskStates, t.DesiredState)
	m["image"] = image
	m["exit_code"] = exitCode
	m["error"] = t.Status.Err
	m["service"] = t.ServiceID
	m["node"] = t.NodeID
	m["slot"] = t.Slot
	return m
}

// ConfigEnv builds an expression environment from a swarm.Config.
func ConfigEnv(c swarm.Config, m map[string]any) map[string]any {
	if m == nil {
		m = make(map[string]any, 2)
	}
	m["id"] = c.ID
	m["name"] = c.Spec.Name
	return m
}

// SecretEnv builds an expression environment from a swarm.Secret.
func SecretEnv(s swarm.Secret, m map[string]any) map[string]any {
	if m == nil {
		m = make(map[string]any, 2)
	}
	m["id"] = s.ID
	m["name"] = s.Spec.Name
	return m
}

// NetworkEnv builds an expression environment from a network.Summary.
func NetworkEnv(n network.Summary, m map[string]any) map[string]any {
	if m == nil {
		m = make(map[string]any, 4)
	}
	m["id"] = n.ID
	m["name"] = n.Name
	m["driver"] = n.Driver
	m["scope"] = n.Scope
	return m
}

// VolumeEnv builds an expression environment from a volume.Volume.
func VolumeEnv(v volume.Volume, m map[string]any) map[string]any {
	if m == nil {
		m = make(map[string]any, 3)
	}
	m["name"] = v.Name
	m["driver"] = v.Driver
	m["scope"] = v.Scope
	return m
}

// StackEnv builds an expression environment from a cache.Stack.
func StackEnv(s cache.Stack, m map[string]any) map[string]any {
	if m == nil {
		m = make(map[string]any, 6)
	}
	m["name"] = s.Name
	m["services"] = len(s.Services)
	m["configs"] = len(s.Configs)
	m["secrets"] = len(s.Secrets)
	m["networks"] = len(s.Networks)
	m["volumes"] = len(s.Volumes)
	return m
}

// ResourceEnv builds an expression environment from an interface{} resource,
// as stored in cache.Event.Resource. Returns nil for unrecognized types.
func ResourceEnv(resource any) map[string]any {
	switch r := resource.(type) {
	case swarm.Node:
		return NodeEnv(r, nil)
	case swarm.Service:
		return ServiceEnv(r, nil)
	case swarm.Task:
		return TaskEnv(r, nil)
	case swarm.Config:
		return ConfigEnv(r, nil)
	case swarm.Secret:
		return SecretEnv(r, nil)
	case network.Summary:
		return NetworkEnv(r, nil)
	case volume.Volume:
		return VolumeEnv(r, nil)
	default:
		return nil
	}
}
