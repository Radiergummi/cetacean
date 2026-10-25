package mcp

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// nodeChoiceKey is the input request, and the form property, a node choice
// is asked under.
const nodeChoiceKey = "node"

// errNotChosen is a person declining to choose.
var errNotChosen = errors.New("the person did not choose")

// option is one answer to a choose question.
type option struct {
	value string
	title string
}

// choose asks a person to pick one of options under key, and returns the
// value picked. An earlier pick carried in the state is reused while the
// options are the same; a changed set is asked about again.
func (s *Server) choose(
	ctx context.Context,
	req mcplib.CallToolRequest,
	key, message string,
	options []option,
) (string, error) {
	values := make([]string, 0, len(options))
	for _, o := range options {
		values = append(values, o.value)
	}

	slices.Sort(values)
	bind := strings.Join(values, ",")

	if prior, ok := s.priorRound(ctx, req); ok {
		if picked, ok := prior.Choices[key]; ok && picked.Bind == bind &&
			slices.Contains(values, picked.Value) {
			return picked.Value, nil
		}

		answer := mcpserver.ElicitationResponse(req.Params.InputResponses, key)
		if answer != nil && prior.asked(key, 0, bind) {
			if answer.Action != mcplib.ElicitationResponseActionAccept {
				return "", errNotChosen
			}

			content, _ := answer.Content.(map[string]any)
			if value, _ := content[key].(string); slices.Contains(values, value) {
				return value, nil
			}
		}
	}

	oneOf := make([]map[string]any, 0, len(options))
	for _, o := range options {
		oneOf = append(oneOf, map[string]any{"const": o.value, "title": o.title})
	}

	builder := mcpserver.NewInputRequestBuilder(s.nextRound(ctx, req, key, 0, bind))
	builder.Elicit(key, mcplib.ElicitationParams{
		Mode:    mcplib.ElicitationModeForm,
		Message: message,
		RequestedSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				key: map[string]any{"type": "string", "title": "Node", "oneOf": oneOf},
			},
			"required": []string{key},
		},
	})

	return "", &toolOutcome{result: builder.ToolResult()}
}

// resolveNodeFor resolves the node identifier a tool was given. An ambiguous
// hostname the caller may read is put to a person as a choice when the client
// can ask; resources keep the error, since their handlers return contents only.
func (s *Server) resolveNodeFor(
	ctx context.Context,
	req mcplib.CallToolRequest,
	identifier string,
) (swarm.Node, bool, error) {
	node, found, err := s.cache.ResolveNode(identifier)

	ambiguous, isAmbiguous := errors.AsType[*cache.AmbiguousNameError](err)
	if !isAmbiguous {
		return node, found, err
	}

	if s.visibleAmbiguity(ctx, "node", err) == nil {
		return swarm.Node{}, false, nil
	}

	readable := s.readPredicate(ctx)
	candidates := map[string]swarm.Node{}

	for _, id := range ambiguous.IDs {
		if n, ok := s.cache.GetNode(id); ok && readable("node:"+cluster.NodeACLName(n)) {
			candidates[id] = n
		}
	}

	switch {
	case len(candidates) == 0:
		return swarm.Node{}, false, nil
	case len(candidates) == 1:
		for _, n := range candidates {
			return n, true, nil
		}
	case !clientElicits(ctx, mcplib.ElicitationModeForm):
		return swarm.Node{}, false, err
	}

	options := make([]option, 0, len(candidates))
	for _, id := range slices.Sorted(maps.Keys(candidates)) {
		options = append(options, option{value: id, title: nodeOptionTitle(candidates[id])})
	}

	chosen, chooseErr := s.choose(ctx, req, nodeChoiceKey, fmt.Sprintf(
		"%q names %d nodes. Which one do you mean?", identifier, len(options),
	), options)
	if errors.Is(chooseErr, errNotChosen) {
		return swarm.Node{}, false, fmt.Errorf("%w which node: %w", chooseErr, err)
	}

	if chooseErr != nil {
		return swarm.Node{}, false, chooseErr
	}

	return candidates[chosen], true, nil
}

// nodeOptionTitle is what tells two nodes with one hostname apart.
func nodeOptionTitle(n swarm.Node) string {
	parts := []string{n.ID}

	if n.Status.Addr != "" {
		parts = append(parts, n.Status.Addr)
	}

	parts = append(parts, string(n.Spec.Role)+", "+string(n.Spec.Availability))

	if n.Description.Engine.EngineVersion != "" {
		parts = append(parts, "Engine "+n.Description.Engine.EngineVersion)
	}

	if len(n.Spec.Labels) > 0 {
		labels := make([]string, 0, len(n.Spec.Labels))
		for _, key := range slices.Sorted(maps.Keys(n.Spec.Labels)) {
			labels = append(labels, key+"="+n.Spec.Labels[key])
		}

		parts = append(parts, strings.Join(labels, " "))
	}

	return strings.Join(parts, " · ")
}

// resolveNodeWrite resolves the node a write names and checks the caller may
// write it, returning the ID to hand Docker. A node the cache does not hold is
// passed on as named when no policy applies, as before.
func (s *Server) resolveNodeWrite(
	ctx context.Context,
	req mcplib.CallToolRequest,
	identifier string,
) (string, error) {
	node, found, err := s.resolveNodeFor(ctx, req, identifier)
	if _, ok := asToolOutcome(err); ok || errors.Is(err, errNotChosen) {
		return "", err
	}

	if err := s.checkResolvedWrite(
		ctx, "node", identifier, cluster.NodeACLName(node), found, err,
	); err != nil {
		return "", err
	}

	if found {
		return node.ID, nil
	}

	return identifier, nil
}
