package mcp

import (
	"github.com/docker/docker/api/types/mount"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/config"
)

// configurationTools returns the tier 2 tools: edits to a service's or
// node's specification, and the creation of the configs and secrets a
// specification can point at.
//
// See toolCatalog for the conventions every entry here follows.
func (s *Server) configurationTools() []toolDef {
	return []toolDef{
		{
			tool: mcplib.NewTool(
				"update_service",
				mcplib.WithToolTitle("Update a service's specification"),
				mcplib.WithDescription(
					"Change one section of a service's specification, named by `section`. `env` and `labels` take a JSON Merge Patch (RFC 7396) object — a string sets or replaces a key, null deletes it, an omitted key is preserved. The other six replace their section wholesale: pass the complete object, because a field you omit is cleared. `resources` takes ResourceRequirements (CPU/memory limits and reservations); `placement` takes Placement (constraints, preferences, max replicas per node, platforms) and may reschedule tasks onto other nodes to satisfy new constraints; `ports` takes an array of PortConfig and drops connections to any port it removes or remaps; `update-policy` and `rollback-policy` take an UpdateConfig (parallelism, delay, failure action, monitor window, max failure ratio, order) and change nothing by themselves, applying to the next spec change and to rollback_service respectively; `log-driver` takes a Driver (name plus options) and routes subsequent log lines through it. `healthcheck` takes a probe: `test` is the command as an array, for instance [`CMD`, `curl`, `-f`, `http://localhost/`], and `interval`, `timeout` and `startPeriod` are duration strings such as `10s` or `1m30s` rather than numbers; an empty `test` removes the healthcheck. `command` takes `command` (the entrypoint) and `args` (what follows it), the split Docker itself makes, and omitting one clears it. Every section except labels and the two policies triggers a rolling deploy. Returns the section as it now stands, not the whole service — describe the service for the rest.",
				),
				mcplib.WithOutputSchema[serviceUpdateResult](),
				mcplib.WithReadOnlyHintAnnotation(false),
				// Destructive because two of the sections are: replacing the
				// ports drops the connections to a port it removes, and
				// replacing the placement can evict running tasks. One
				// annotation covers every section, so it has to describe the
				// worst of them — a host deciding whether to confirm must not
				// be told a port remap is safe because a label edit is.
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithString("section",
					mcplib.Required(),
					// The advertised enum is the accepted list itself, not a
					// third copy of it: a section added to the slice and the
					// switch but forgotten here yields a tool the server
					// accepts and every schema-validating client refuses.
					mcplib.Enum(updateServiceSections...),
					mcplib.Description("Which part of the specification to change."),
				),
				mcplib.WithAny("value",
					mcplib.Required(),
					mcplib.Description(
						"The section's new value: a merge-patch object for `env` and `labels`, an array of PortConfig for `ports`, and the replacement object for the rest. See the tool description for the shape each section expects.",
					),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateService,
		},
		{
			tool: mcplib.NewTool("update_node_labels",
				mcplib.WithToolTitle("Update node labels"),
				mcplib.WithDescription(
					"Patch the labels on a node using JSON Merge Patch (RFC 7396) semantics: string values set or replace a key, null values delete it, omitted keys are preserved. Node labels are commonly used as service placement constraints. Returns the updated node spec.",
				),
				mcplib.WithOutputSchema[nodeUpdateResult](),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Node ID or hostname."),
				),
				mcplib.WithObject(
					"labels",
					mcplib.Required(),
					mcplib.Description(
						"Merge-patch object mapping label keys to a string value (set) or null (delete).",
					),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateNodeLabels,
		},
		{
			tool: mcplib.NewTool(
				"create_secret",
				mcplib.WithToolTitle("Create a secret"),
				mcplib.WithDescription(
					"Create a Swarm secret. Swarm secrets cannot be changed once created, so rotating one is three calls in order: this tool for the replacement, update_service_secrets to repoint each service that uses the old one (describe the old secret first — its related array names them), then remove_secret once nothing references it. The value is write-only: no tool ever returns it, including this one. Pass encoding as base64 for binary content or anything JSON string escaping would mangle, such as a certificate or a key file; without it the value is stored exactly as given, since plenty of ordinary passwords are also valid base64 and guessing would silently store the wrong thing.",
				),
				mcplib.WithOutputSchema[createResult](),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString(
					"name",
					mcplib.Required(),
					mcplib.Description(
						"Name for the new secret. Must not already exist in the cluster.",
					),
				),
				mcplib.WithString("data",
					mcplib.Required(),
					mcplib.Description("The secret's value."),
				),
				mcplib.WithString(
					"encoding",
					mcplib.Enum("utf8", "base64"),
					mcplib.Description(
						"How `data` is encoded. Defaults to utf8, which stores it verbatim.",
					),
				),
				mcplib.WithObject(
					"labels",
					mcplib.Description(
						"Labels to set on the secret. Set com.docker.stack.namespace to attach it to a stack.",
					),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.createHandler(secretResource),
		},
		{
			tool: mcplib.NewTool(
				"create_config",
				mcplib.WithToolTitle("Create a config"),
				mcplib.WithDescription(
					"Create a Swarm config. Like a secret, a config cannot be changed once created — replace it by creating a new one, repointing services with update_service_configs, and removing the old one. Unlike a secret, its content can be read back afterwards, so use a secret for anything sensitive. Pass encoding as base64 for a file whose content JSON string escaping would mangle.",
				),
				mcplib.WithOutputSchema[createResult](),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString(
					"name",
					mcplib.Required(),
					mcplib.Description(
						"Name for the new config. Must not already exist in the cluster.",
					),
				),
				mcplib.WithString("data",
					mcplib.Required(),
					mcplib.Description("The config's content."),
				),
				mcplib.WithString(
					"encoding",
					mcplib.Enum("utf8", "base64"),
					mcplib.Description(
						"How `data` is encoded. Defaults to utf8, which stores it verbatim.",
					),
				),
				mcplib.WithObject(
					"labels",
					mcplib.Description(
						"Labels to set on the config. Set com.docker.stack.namespace to attach it to a stack.",
					),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.createHandler(configResource),
		},
		{
			tool: mcplib.NewTool(
				"update_service_secrets",
				mcplib.WithToolTitle("Change which secrets a service receives"),
				mcplib.WithDescription(
					"Replace the complete set of secrets a service's containers receive. Swarm secrets cannot be changed in place, so rotating one is: create_secret for the replacement, this tool to repoint each service that uses the old one, then remove_secret once nothing references it. Describe the old secret first — its related array names the services to repoint. The list replaces rather than merges: pass every secret the service should end up with, because one you leave out is detached and the container loses it. Each entry names a secret rather than giving its ID. Triggers a rolling deploy, and you must be permitted to read a secret in order to attach it.",
				),
				mcplib.WithOutputSchema[serviceUpdateResult](),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithArray("secrets",
					mcplib.Required(),
					mcplib.Description(
						"The complete set of secrets the service should receive. Each entry takes `name` (the secret's name) and an optional `target` (the file it is mounted as, relative to /run/secrets unless absolute; defaults to /run/secrets/<name>). An empty array detaches every secret.",
					),
					mcplib.Items(attachmentItemSchema()),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateServiceSecrets,
		},
		{
			tool: mcplib.NewTool(
				"update_service_configs",
				mcplib.WithToolTitle("Change which configs a service receives"),
				mcplib.WithDescription(
					"Replace the complete set of configs a service's containers receive. Like secrets, Swarm configs cannot be changed in place: create the replacement with create_config, repoint the services here, then remove the old one. The list replaces rather than merges, so pass every config the service should end up with — one you leave out is detached. Each entry names a config rather than giving its ID. Triggers a rolling deploy, and you must be permitted to read a config in order to attach it.",
				),
				mcplib.WithOutputSchema[serviceUpdateResult](),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithArray("configs",
					mcplib.Required(),
					mcplib.Description(
						"The complete set of configs the service should receive. Each entry takes `name` (the config's name) and an optional `target` (the absolute path it is mounted at; defaults to /<name>). An empty array detaches every config.",
					),
					mcplib.Items(attachmentItemSchema()),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateServiceConfigs,
		},
		{
			tool: mcplib.NewTool(
				"update_service_mounts",
				mcplib.WithToolTitle("Change a service's filesystem mounts"),
				mcplib.WithDescription(
					"Replace the complete set of filesystem mounts a service's containers receive: named volumes, host bind mounts and tmpfs. The list replaces rather than merges, so pass every mount the service should end up with — one you leave out is unmounted, and a container may lose data it was writing there. Triggers a rolling deploy. Note that a bind mount hands the container the host's filesystem at that path, and binding the Docker socket (/var/run/docker.sock) gives it control of the whole cluster; this tool will do it if asked, so check with the operator before mounting a host path they did not name.",
				),
				mcplib.WithOutputSchema[serviceUpdateResult](),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithArray("mounts",
					mcplib.Required(),
					mcplib.Description(
						"The complete set of mounts the service should receive. Each entry takes `type` (volume, bind or tmpfs), `target` (the absolute path inside the container), an optional `source` (the volume name for a volume, the host path for a bind; a tmpfs has none) and an optional `readOnly`. An empty array unmounts everything.",
					),
					mcplib.Items(map[string]any{
						"type": "object",
						"properties": map[string]any{
							"type": map[string]any{
								"type": "string",
								"enum": []string{
									string(mount.TypeVolume),
									string(mount.TypeBind),
									string(mount.TypeTmpfs),
								},
							},
							"source":   map[string]any{"type": "string"},
							"target":   map[string]any{"type": "string"},
							"readOnly": map[string]any{"type": "boolean"},
						},
						"required": []string{"type", "target"},
					}),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateServiceMounts,
		},
	}
}
