package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/mcp/projection"
)

// annotationResult is an annotation as the annotation tools and resources
// show it, its times in the format get_events uses. Version is what
// update_annotation and delete_annotation take.
type annotationResult struct {
	ID        uint64                     `json:"id"`
	Version   uint64                     `json:"version"`
	At        string                     `json:"at"`
	Until     string                     `json:"until,omitempty"`
	Text      string                     `json:"text"`
	Tags      []string                   `json:"tags"`
	Resources []cache.AnnotationResource `json:"resources"`
	Actor     *cache.Actor               `json:"actor,omitempty"`
	Updated   string                     `json:"updated,omitempty"`
	Editor    *cache.Actor               `json:"editor,omitempty"`
}

// deletedAnnotation is what delete_annotation answers with.
type deletedAnnotation struct {
	ID      uint64 `json:"id"`
	Deleted bool   `json:"deleted"`
}

func annotationResultOf(a cache.Annotation) annotationResult {
	result := annotationResult{
		ID:        a.ID,
		Version:   a.Version,
		At:        projection.TimelineTime(a.Timestamp),
		Text:      a.Text,
		Tags:      a.Tags,
		Resources: a.Resources,
		Actor:     a.Actor,
		Editor:    a.Editor,
	}
	if result.Tags == nil {
		result.Tags = []string{}
	}
	if result.Resources == nil {
		result.Resources = []cache.AnnotationResource{}
	}
	if !a.Until.IsZero() {
		result.Until = projection.TimelineTime(a.Until)
	}
	if !a.Updated.IsZero() {
		result.Updated = projection.TimelineTime(a.Updated)
	}

	return result
}

// annotationTools has no op: an annotation changes nothing in Docker, so the
// tools are registered at every level, and cluster.Annotations checks the ACL.
// They are withheld without storage.timeline, where every call would fail.
func (s *Server) annotationTools() []toolDef {
	enabled := s.annotations.Enabled

	return []toolDef{
		{
			available: enabled,
			tool: mcplib.NewTool(
				"add_annotation",
				mcplib.WithToolTitle("Annotate the timeline"),
				mcplib.WithDescription(
					"Record a note on the cluster's timeline about a moment or a range: that a load test started, why a deploy was rolled back, what an incident turned out to be. People see it on the charts and history of each resource it names, and get_events returns it to anyone reading that time later. It changes nothing in the cluster. It needs write on every resource it names, or on everything for a cluster-wide note naming none, and it needs storage.timeline. It is attributed to the caller, via MCP. The result's `id` and `version` are what update_annotation and delete_annotation take.",
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
						"RFC 3339 timestamp of the moment, or the start of the range, in the years 1970 to 2199. Defaults to now.",
					),
				),
				mcplib.WithString("until",
					mcplib.Description(
						"RFC 3339 timestamp ending a range, after `at`. Omit for a moment.",
					),
				),
				annotationTagsArg("At most 10 tags"),
				annotationResourcesArg(
					"At most 20 resources the note is about, each a service, stack or node named by its ID or its name. Omit for a cluster-wide note.",
				),
			),
			handler: s.toolAddAnnotation,
		},
		{
			available: enabled,
			tool: mcplib.NewTool(
				"update_annotation",
				mcplib.WithToolTitle("Edit a timeline annotation"),
				mcplib.WithDescription(
					"Change an annotation: its text, its time or range, its tags or the resources it is about. Only the arguments given change. Find the annotation's `id` and `version` with get_events, kinds [\"annotation\"], or read cetacean://annotations/{id}. The author may edit it while they can still read it; anyone else needs what creating it would, and changing `resources` needs that for the new set too. The edit is attributed to the caller, via MCP.",
				),
				mcplib.WithOutputSchema[annotationResult](),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithInteger("id",
					mcplib.Required(),
					mcplib.Min(1),
					mcplib.Description("The annotation's id."),
				),
				mcplib.WithString("text",
					mcplib.Description("The new text, at most 1000 characters."),
				),
				mcplib.WithString(
					"at",
					mcplib.Description(
						"RFC 3339 timestamp of the moment, or the start of the range, in the years 1970 to 2199.",
					),
				),
				mcplib.WithString("until",
					mcplib.Description(
						"RFC 3339 timestamp ending the range, after `at`. An empty string makes the annotation a moment.",
					),
				),
				annotationTagsArg(
					"The tags in full, replacing the current ones; at most 10, and an empty list clears them",
				),
				annotationResourcesArg(
					"The resources in full, replacing the current ones; at most 20, and an empty list makes the note cluster-wide.",
				),
				annotationVersionArg(),
			),
			handler: s.toolUpdateAnnotation,
		},
		{
			available: enabled,
			tool: mcplib.NewTool(
				"delete_annotation",
				mcplib.WithToolTitle("Delete a timeline annotation"),
				mcplib.WithDescription(
					"Remove an annotation from the timeline for everyone; it cannot be restored. Who may: the same rule as update_annotation. It changes nothing in the cluster.",
				),
				mcplib.WithOutputSchema[deletedAnnotation](),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithInteger("id",
					mcplib.Required(),
					mcplib.Min(1),
					mcplib.Description("The annotation's id."),
				),
				annotationVersionArg(),
			),
			handler: s.toolDeleteAnnotation,
		},
	}
}

func annotationTagsArg(limit string) mcplib.ToolOption {
	return mcplib.WithArray(
		"tags",
		mcplib.Description(limit+", each 1 to 40 of a-z, 0-9, '.', '_' and '-'."),
		mcplib.Items(map[string]any{"type": "string"}),
	)
}

func annotationResourcesArg(description string) mcplib.ToolOption {
	return mcplib.WithArray("resources",
		mcplib.Description(description),
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
	)
}

func annotationVersionArg() mcplib.ToolOption {
	return mcplib.WithInteger("version",
		mcplib.Min(1),
		mcplib.Description(
			"The annotation's `version` as you last read it. When given, the call applies only if nobody has written the annotation since; otherwise it is refused with the current version and nothing changes. Omit it to change whatever the annotation holds now.",
		),
	)
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

	resources, err := annotationRefs(req.GetArguments()["resources"])
	if err != nil {
		return "", err
	}

	a, err := s.annotations.Create(ctx, s.annotationCaller(ctx), cluster.AnnotationInput{
		Timestamp: at,
		Until:     until,
		Text:      text,
		Tags:      tags,
		Resources: resources,
	})
	if err != nil {
		return "", annotationError(0, err)
	}

	return marshalResult(annotationResultOf(a))
}

func (s *Server) toolUpdateAnnotation(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := annotationIDArg(req)
	if err != nil {
		return "", err
	}

	patch, err := annotationPatchOf(req.GetArguments())
	if err != nil {
		return "", err
	}

	matches, err := annotationVersionCondition(req)
	if err != nil {
		return "", err
	}

	caller := s.annotationCaller(ctx)

	a, err := s.annotations.Update(ctx, caller, id, patch, matches)
	if err != nil {
		return "", s.annotationWriteError(caller, id, err)
	}

	return marshalResult(annotationResultOf(a))
}

func (s *Server) toolDeleteAnnotation(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := annotationIDArg(req)
	if err != nil {
		return "", err
	}

	matches, err := annotationVersionCondition(req)
	if err != nil {
		return "", err
	}

	caller := s.annotationCaller(ctx)

	if err := s.annotations.Delete(ctx, caller, id, matches); err != nil {
		return "", s.annotationWriteError(caller, id, err)
	}

	return marshalResult(deletedAnnotation{ID: id, Deleted: true})
}

// annotationPatchOf reads the members update_annotation was given; one left
// out is no change. An empty `until` clears the range.
func annotationPatchOf(arguments map[string]any) (cluster.AnnotationPatch, error) {
	var patch cluster.AnnotationPatch

	if raw, ok := arguments["text"]; ok {
		text, isString := raw.(string)
		if !isString {
			return patch, errors.New("text: must be a string")
		}

		patch.Text = &text
	}

	if raw, ok := arguments["at"]; ok {
		value, _ := raw.(string)
		if value == "" {
			return patch, errors.New("at: cannot be removed; give the new start")
		}

		at, err := annotationTime("at", value)
		if err != nil {
			return patch, err
		}

		patch.Timestamp = &at
	}

	if raw, ok := arguments["until"]; ok {
		value, _ := raw.(string)

		until, err := annotationTime("until", value)
		if err != nil {
			return patch, err
		}

		patch.Until = &until
	}

	if raw, ok := arguments["tags"]; ok {
		tags, err := annotationTags(raw)
		if err != nil {
			return patch, err
		}

		if tags == nil {
			tags = []string{}
		}

		patch.Tags = &tags
	}

	if raw, ok := arguments["resources"]; ok {
		resources, err := annotationRefs(raw)
		if err != nil {
			return patch, err
		}

		patch.Resources = &resources
	}

	return patch, nil
}

func annotationRefs(raw any) ([]cluster.AnnotationRef, error) {
	resources := []cluster.AnnotationRef{}
	if raw == nil {
		return resources, nil
	}

	encoded, _ := json.Marshal(raw)
	if err := json.Unmarshal(encoded, &resources); err != nil {
		return nil, errors.New("resources: each must be an object with a type and an id")
	}

	return resources, nil
}

func annotationIDArg(req mcplib.CallToolRequest) (uint64, error) {
	number, ok := req.GetArguments()["id"].(float64)
	if !ok || number < 1 || number != math.Trunc(number) || number > 1<<53 {
		return 0, errors.New("id: must be an annotation's id, a positive integer")
	}

	return uint64(number), nil
}

// annotationVersionCondition is the precondition `version` asks for, or nil.
// Over MCP it is the version alone: the removal marks REST's ETag carries
// change what a read shows, not what the annotation holds.
func annotationVersionCondition(req mcplib.CallToolRequest) (func(cache.Annotation) bool, error) {
	raw, present := req.GetArguments()["version"]
	if !present || raw == nil {
		return nil, nil
	}

	number, ok := raw.(float64)
	if !ok || number < 1 || number != math.Trunc(number) || number > 1<<53 {
		return nil, fmt.Errorf(
			"version: must be the annotation's version, a positive integer; got %v",
			raw,
		)
	}

	version := uint64(number)

	return func(a cache.Annotation) bool { return a.Version == version }, nil
}

// annotationWriteError explains a refused edit or delete; a stale version
// is answered with the current one, so the caller can read and retry.
func (s *Server) annotationWriteError(caller cluster.AnnotationCaller, id uint64, err error) error {
	if errors.Is(err, cluster.ErrAnnotationPrecondition) {
		if current, readErr := s.annotations.Get(caller, id); readErr == nil {
			return fmt.Errorf(
				"annotation %d changed since that version; it is now at version %d, and nothing was written",
				id,
				current.Version,
			)
		}
	}

	return annotationError(id, err)
}

// annotationError words the use case's refusals for an agent.
func annotationError(id uint64, err error) error {
	switch {
	case errors.Is(err, cluster.ErrAnnotationDenied):
		return errors.New(
			"write access denied: an annotation needs write on every resource it names, " +
				"or on everything when it names none; its author may also change it",
		)
	case errors.Is(err, cache.ErrAnnotationNotFound):
		return fmt.Errorf("annotation %d not found", id)
	case errors.Is(err, cache.ErrAnnotationsOff):
		return errors.New("annotations need storage.timeline, which is off")
	default:
		return err
	}
}

// readAnnotation serves cetacean://annotations/{id} and the tag list at
// cetacean://annotations/tags, to a caller with any grant.
func (s *Server) readAnnotation(ctx context.Context, uri, id string) (any, error) {
	if err := s.requireAnyGrant(ctx); err != nil {
		return nil, err
	}

	if !s.annotations.Enabled() {
		return nil, annotationError(0, cache.ErrAnnotationsOff)
	}

	caller := s.annotationCaller(ctx)

	if id == "tags" {
		tags := s.cache.AnnotationTags("", caller.MayRead)
		if tags == nil {
			tags = []cache.AnnotationTag{}
		}

		return tags, nil
	}

	number, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return nil, notFound(uri)
	}

	a, err := s.annotations.Get(caller, number)
	if err != nil {
		return nil, notFound(uri)
	}

	return annotationResultOf(a), nil
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

// annotationCallerFor is who reads an annotation for a subscriber, who has
// an identity but no request.
func (s *Server) annotationCallerFor(identity *auth.Identity) cluster.AnnotationCaller {
	return cluster.AnnotationCaller{
		CanRead:  func(resource string) bool { return s.canRead(identity, resource) },
		AnyGrant: identity == nil || s.acl == nil || s.acl.HasAnyGrant(identity),
	}
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
