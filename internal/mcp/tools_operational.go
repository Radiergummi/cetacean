package mcp

import (
	"context"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/config"
)

// operationalTools returns the tier 1 tools: changes to how much of a
// service runs and where, none of which alter its specification.
//
// See toolCatalog for the conventions every entry here follows.
func (s *Server) operationalTools() []toolDef {
	return []toolDef{
		{
			tool: mcplib.NewTool(
				"scale_service",
				mcplib.WithToolTitle("Scale service replicas"),
				mcplib.WithDescription(
					"Set the desired replica count of a replicated service. Swarm reconciles asynchronously; calling with the current count is a no-op. Setting to 0 stops every task without removing the service. Returns a summary of where the service ended up: id, name, image, mode, desired replicas, running count, derived state and version.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				taskSupportOptional(),
				mcplib.WithOutputSchema[serviceMutationResult](),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithNumber("replicas",
					mcplib.Required(),
					mcplib.Description("Target replica count. Must be >= 0."),
				),
			),
			tier:    config.OpsOperational,
			handler: s.toolScaleService,
		},
		{
			tool: mcplib.NewTool(
				"update_service_image",
				mcplib.WithToolTitle("Update service image"),
				mcplib.WithDescription(
					"Set the container image for a service, triggering a rolling deploy per the service's update policy. The previous spec is retained on the service history and reachable via rollback_service. Returns a summary of where the service ended up: id, name, image, mode, desired replicas, running count, derived state and version.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				taskSupportOptional(),
				mcplib.WithOutputSchema[serviceMutationResult](),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithString(
					"image",
					mcplib.Required(),
					mcplib.Description(
						"Container image reference including tag or digest (e.g. \"nginx:1.27\" or \"nginx@sha256:...\"). Must not be empty.",
					),
				),
			),
			tier:    config.OpsOperational,
			handler: s.toolUpdateServiceImage,
		},
		{
			tool: mcplib.NewTool("rollback_service",
				mcplib.WithToolTitle("Roll back service to previous spec"),
				mcplib.WithDescription(
					"Revert a service to its previous spec, discarding the current one. Only the immediately previous spec is retained — calling twice does not unwind further. Returns a summary of where the service ended up: id, name, image, mode, desired replicas, running count, derived state and version.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				taskSupportOptional(),
				mcplib.WithOutputSchema[serviceMutationResult](),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
			),
			tier:    config.OpsOperational,
			handler: s.toolRollbackService,
		},
		{
			tool: mcplib.NewTool("restart_service",
				mcplib.WithToolTitle("Force-restart service"),
				mcplib.WithDescription(
					"Force a rolling restart of every task in the service by bumping its spec ForceUpdate counter. No other spec fields change. Honours the service's update policy (parallelism, delay). Returns a summary of where the service ended up: id, name, image, mode, desired replicas, running count, derived state and version.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				taskSupportOptional(),
				mcplib.WithOutputSchema[serviceMutationResult](),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
			),
			tier:    config.OpsOperational,
			handler: s.toolRestartService,
		},
		{
			tool: mcplib.NewTool("remove_task",
				mcplib.WithToolTitle("Remove (reschedule) task"),
				mcplib.WithOutputSchema[removalResult](),
				mcplib.WithDescription(
					"Delete a task by ID. Swarm immediately reschedules a replacement on the parent service's behalf, so this is a forced reschedule rather than a permanent removal. Use this to evict a misbehaving task without scaling the whole service.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Task ID (long-form Docker ID)."),
				),
			),
			tier: config.OpsOperational,
			handler: s.removeHandler("id", s.checkTaskWrite,
				func(wc DockerWriteClient, ctx context.Context, id string) error {
					return wc.RemoveTask(ctx, id)
				}),
		},
	}
}
