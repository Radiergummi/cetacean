package mcp

import (
	"context"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/config"
)

// impactfulTools returns the tier 3 tools: removals, and the node changes
// that move work off a host.
//
// See toolCatalog for the conventions every entry here follows.
func (s *Server) impactfulTools() []toolDef {
	return []toolDef{
		{
			tool: mcplib.NewTool(
				"update_node",
				mcplib.WithToolTitle("Update a node's role or availability"),
				mcplib.WithDescription(
					"Change a node's scheduling state, named by `section`. `availability` takes one of `active` (accepts new tasks), `pause` (rejects new tasks, keeps existing ones running) or `drain` (evicts every task to other nodes); draining a manager may destabilise the swarm, so prefer draining workers. `role` takes `worker` or `manager`: promoting adds a Raft voter, demoting removes one — never reduce the manager count below the quorum threshold, and for a three-manager cluster demote at most one at a time. Returns the node's role and availability as they now stand. Node labels are edited with update_node_labels, which needs a lower operations level than this.",
				),
				mcplib.WithOutputSchema[nodeUpdateResult](),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Node ID or hostname."),
				),
				mcplib.WithString("section",
					mcplib.Required(),
					mcplib.Enum(sectionAvailability, sectionRole),
					mcplib.Description("Which part of the node's scheduling state to change."),
				),
				mcplib.WithString("value",
					mcplib.Required(),
					mcplib.Description(
						"For `availability`: active, pause or drain. For `role`: worker or manager.",
					),
				),
			),
			tier:    config.OpsImpactful,
			handler: s.toolUpdateNode,
		},
		{
			tool: mcplib.NewTool("remove_service",
				mcplib.WithToolTitle("Remove service"),
				mcplib.WithOutputSchema[removalResult](),
				mcplib.WithDescription(
					"Delete a service and stop every task it owns. Does not delete the volumes, networks, configs, or secrets it referenced. Irreversible — no spec history is retained after removal.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
			),
			tier: config.OpsImpactful,
			handler: s.removeHandler("id", s.checkServiceWrite,
				func(wc DockerWriteClient, ctx context.Context, id string) error {
					return wc.RemoveService(ctx, id)
				}),
		},
		{
			tool: mcplib.NewTool("remove_config",
				mcplib.WithToolTitle("Remove config"),
				mcplib.WithOutputSchema[removalResult](),
				mcplib.WithDescription(
					"Delete a Docker config object. Fails if any service still references it. Irreversible — the config payload cannot be recovered.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Config ID."),
				),
			),
			tier: config.OpsImpactful,
			handler: s.removeHandler("id", s.checkConfigWrite,
				func(wc DockerWriteClient, ctx context.Context, id string) error {
					return wc.RemoveConfig(ctx, id)
				}),
		},
		{
			tool: mcplib.NewTool("remove_secret",
				mcplib.WithToolTitle("Remove secret"),
				mcplib.WithOutputSchema[removalResult](),
				mcplib.WithDescription(
					"Delete a Docker secret. Fails if any service still references it. Irreversible — the secret payload cannot be recovered.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Secret ID."),
				),
			),
			tier: config.OpsImpactful,
			handler: s.removeHandler("id", s.checkSecretWrite,
				func(wc DockerWriteClient, ctx context.Context, id string) error {
					return wc.RemoveSecret(ctx, id)
				}),
		},
		{
			tool: mcplib.NewTool("remove_network",
				mcplib.WithToolTitle("Remove network"),
				mcplib.WithOutputSchema[removalResult](),
				mcplib.WithDescription(
					"Delete an overlay network. Fails if any service is still attached. Built-in networks (`ingress`, `docker_gwbridge`) cannot be removed.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Network ID."),
				),
			),
			tier: config.OpsImpactful,
			handler: s.removeHandler("id", s.checkNetworkWrite,
				func(wc DockerWriteClient, ctx context.Context, id string) error {
					return wc.RemoveNetwork(ctx, id)
				}),
		},
		{
			tool: mcplib.NewTool("remove_volume",
				mcplib.WithToolTitle("Remove volume"),
				mcplib.WithOutputSchema[removalResult](),
				mcplib.WithDescription(
					"Delete a Docker volume by name. Fails if any container is currently using it unless `force` is set. Volume contents are irrecoverable.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("name",
					mcplib.Required(),
					mcplib.Description("Volume name."),
				),
				mcplib.WithBoolean(
					"force",
					mcplib.Description(
						"Force removal even if the volume is in use. Default false.",
					),
				),
			),
			tier:    config.OpsImpactful,
			handler: s.toolRemoveVolume,
		},
	}
}
