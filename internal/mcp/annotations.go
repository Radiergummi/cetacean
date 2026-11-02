package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/mcp/projection"
)

// annotationResult is what add_annotation answers with, its times in the
// format get_events uses.
type annotationResult struct {
	ID        uint64                     `json:"id"`
	At        string                     `json:"at"`
	Until     string                     `json:"until,omitempty"`
	Text      string                     `json:"text"`
	Tags      []string                   `json:"tags"`
	Resources []cache.AnnotationResource `json:"resources"`
}

// annotationTools has no op: an annotation changes nothing in Docker, so it is
// registered at every level, and cluster.Annotations checks the ACL.
func (s *Server) annotationTools() []toolDef {
	return []toolDef{
		{
			tool: mcplib.NewTool(
				"add_annotation",
				mcplib.WithToolTitle("Annotate the timeline"),
				mcplib.WithDescription(
					"Record a note on the cluster's timeline about a moment or a range: that a load test started, why a deploy was rolled back, what an incident turned out to be. People see it on the charts and history of each resource it names, and get_events returns it to anyone reading that time later. It changes nothing in the cluster. It needs write on every resource it names, or on everything for a cluster-wide note naming none, and it needs storage.timeline. It is attributed to the caller, via MCP.",
				),
				mcplib.WithOutputSchema[annotationResult](),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("text",
					mcplib.Required(),
					mcplib.Description("The note, as plain text of at most 1000 characters."),
				),
				mcplib.WithString("at",
					mcplib.Description(
						"RFC 3339 timestamp of the moment, or the start of the range. Defaults to now.",
					),
				),
				mcplib.WithString("until",
					mcplib.Description(
						"RFC 3339 timestamp ending a range, after `at`. Omit for a moment.",
					),
				),
				mcplib.WithArray(
					"tags",
					mcplib.Description(
						"At most 10 tags, each 1 to 40 of a-z, 0-9, '.', '_' and '-'.",
					),
					mcplib.Items(map[string]any{"type": "string"}),
				),
				mcplib.WithArray("resources",
					mcplib.Description(
						"At most 20 resources the note is about, each a service, stack or node named by its ID or its name. Omit for a cluster-wide note.",
					),
					mcplib.Items(map[string]any{
						"type":     "object",
						"required": []string{"type", "id"},
						"properties": map[string]any{
							"type": map[string]any{
								"type": "string",
								"enum": []string{"service", "stack", "node"},
							},
							"id": map[string]any{"type": "string"},
						},
					}),
				),
			),
			handler: s.toolAddAnnotation,
		},
	}
}

func (s *Server) toolAddAnnotation(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	text, err := req.RequireString("text")
	if err != nil {
		return "", err
	}

	at, err := annotationTime("at", req.GetString("at", ""))
	if err != nil {
		return "", err
	}

	until, err := annotationTime("until", req.GetString("until", ""))
	if err != nil {
		return "", err
	}

	tags, err := annotationTags(req.GetArguments()["tags"])
	if err != nil {
		return "", err
	}

	var resources []cluster.AnnotationRef
	if raw, ok := req.GetArguments()["resources"]; ok {
		encoded, _ := json.Marshal(raw)
		if err := json.Unmarshal(encoded, &resources); err != nil {
			return "", errors.New("resources: each must be an object with a type and an id")
		}
	}

	a, err := s.annotations.Create(ctx, s.annotationCaller(ctx), cluster.AnnotationInput{
		Timestamp: at,
		Until:     until,
		Text:      text,
		Tags:      tags,
		Resources: resources,
	})
	if errors.Is(err, cluster.ErrAnnotationDenied) {
		return "", errors.New(
			"write access denied: an annotation needs write on every resource it names, " +
				"or on everything when it names none",
		)
	}
	if err != nil {
		return "", err
	}

	result := annotationResult{
		ID:        a.ID,
		At:        projection.TimelineTime(a.Timestamp),
		Text:      a.Text,
		Tags:      a.Tags,
		Resources: a.Resources,
	}
	if !a.Until.IsZero() {
		result.Until = projection.TimelineTime(a.Until)
	}

	return marshalResult(result)
}

// annotationTags reads `tags`, refusing anything but a list of strings rather
// than dropping it, so a note is never stored without the tags it was given.
func annotationTags(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}

	items, ok := raw.([]any)
	tags := make([]string, 0, len(items))
	for _, item := range items {
		tag, isString := item.(string)
		ok = ok && isString
		tags = append(tags, tag)
	}
	if !ok {
		return nil, errors.New("tags: must be a list of strings")
	}

	return tags, nil
}

func annotationTime(name, raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}

	parsed, err := time.Parse(time.RFC3339, strings.ToUpper(raw))
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"%s: must be an RFC 3339 timestamp, such as 2026-09-05T14:02:00Z",
			name,
		)
	}

	return parsed, nil
}

// annotationCaller is who reads or writes an annotation over MCP. No identity
// reads and writes everything, as everywhere else in this package.
func (s *Server) annotationCaller(ctx context.Context) cluster.AnnotationCaller {
	identity := auth.IdentityFromContext(ctx)

	return cluster.AnnotationCaller{
		CanRead:          s.readPredicate(ctx),
		CanWrite:         s.writePredicate(ctx),
		AnyGrant:         identity == nil || s.acl.HasAnyGrant(identity),
		WritesEverything: identity == nil || s.acl.CanWriteEverything(identity),
	}
}
