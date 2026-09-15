package mcp

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// defaultAttachmentMode is the file mode Swarm gives a mounted secret or
// config: readable by everyone in the container, writable by no one. It
// matches what the REST handler writes, so the same attachment made through
// either transport lands identically.
const defaultAttachmentMode os.FileMode = 0o444

// attachmentRef is one secret or config a container should receive. It names
// the resource rather than giving its ID, because a model holds names: every
// listing, completion and describe hands one back. Docker's reference type
// wants both and rejects a disagreeing pair, so the ID is resolved here.
type attachmentRef struct {
	Name string `json:"name"`

	// Target is the file the resource is mounted as. A secret's target is
	// relative to /run/secrets unless it is an absolute path; a config's is
	// the path itself. Left empty, it defaults to defaultSecretDir or the
	// container root plus the resource's own name.
	Target string `json:"target,omitempty"`
}

// Where a secret and a config land when the caller names no target: the paths
// REST's PATCH routes default to and Docker's CLI produces. Swarm reads a bare
// config name as relative to the container's working directory, so without
// these the same attachment mounts at two paths over the two transports.
const (
	defaultSecretDir = "/run/secrets/"
	defaultConfigDir = "/"
)

// file builds the mount target, defaulting to dir + the resource's own name.
func (a attachmentRef) file(dir, resourceName string) string {
	if a.Target != "" {
		return a.Target
	}

	return dir + resourceName
}

// attachmentItemSchema is the JSON Schema for one entry of either array. It is
// built here, beside decodeAttachments and attachmentRef, so the shape the
// tools advertise and the shape they decode into cannot drift apart.
func attachmentItemSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":   map[string]any{"type": "string"},
			"target": map[string]any{"type": "string"},
		},
		"required": []string{"name"},
	}
}

// decodeAttachments reads the array argument both tools take.
func decodeAttachments(req mcplib.CallToolRequest, key string) ([]attachmentRef, error) {
	var refs []attachmentRef
	if err := decodeArgInto(req, key, &refs); err != nil {
		return nil, err
	}

	for i, ref := range refs {
		if ref.Name == "" {
			return nil, fmt.Errorf("%s[%d]: name is required", key, i)
		}
	}

	return refs, nil
}

// toolUpdateServiceSecrets replaces the set of secrets a service receives —
// the second of the three calls a rotation needs, since Swarm secrets are
// immutable. Configuration level, matching the REST route. The list replaces
// rather than merges: an empty one detaches every secret.
func (s *Server) toolUpdateServiceSecrets(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}

	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}

	writeClient, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}

	refs, err := decodeAttachments(req, "secrets")
	if err != nil {
		return "", err
	}

	resolved := make([]*swarm.SecretReference, 0, len(refs))

	for _, ref := range refs {
		sec, found, resolveErr := s.cache.ResolveSecret(ref.Name)
		if resolveErr != nil {
			return "", resolveErr
		}
		if !found {
			return "", fmt.Errorf(
				"no such secret %q; create it with create_secret, "+
					"or list what exists with find and type \"secrets\"",
				ref.Name,
			)
		}

		// The caller must be permitted to read a secret to attach it.
		// Without this, a service write grant would be a way to mount a
		// credential the caller cannot otherwise see.
		if err := s.checkRead(ctx, "secret", sec.Spec.Name); err != nil {
			return "", err
		}

		resolved = append(resolved, &swarm.SecretReference{
			SecretID:   sec.ID,
			SecretName: sec.Spec.Name,
			File: &swarm.SecretReferenceFileTarget{
				Name: ref.file(defaultSecretDir, sec.Spec.Name),
				UID:  "0",
				GID:  "0",
				Mode: defaultAttachmentMode,
			},
		})
	}

	updated, err := writeClient.UpdateServiceSecrets(ctx, id, resolved)
	if err != nil {
		return "", fmt.Errorf("update service secrets: %w", err)
	}

	return marshalResult(serviceUpdate(sectionSecrets, updated))
}

// toolUpdateServiceConfigs is the config counterpart, differing only in the
// reference type and the read grant it checks. A config's content is readable
// so attaching one discloses less, but the grant is still required.
func (s *Server) toolUpdateServiceConfigs(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}

	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}

	writeClient, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}

	refs, err := decodeAttachments(req, "configs")
	if err != nil {
		return "", err
	}

	resolved := make([]*swarm.ConfigReference, 0, len(refs))

	for _, ref := range refs {
		cfg, found, resolveErr := s.cache.ResolveConfig(ref.Name)
		if resolveErr != nil {
			return "", resolveErr
		}
		if !found {
			return "", fmt.Errorf(
				"no such config %q; create it with create_config, "+
					"or list what exists with find and type \"configs\"",
				ref.Name,
			)
		}

		if err := s.checkRead(ctx, "config", cfg.Spec.Name); err != nil {
			return "", err
		}

		resolved = append(resolved, &swarm.ConfigReference{
			ConfigID:   cfg.ID,
			ConfigName: cfg.Spec.Name,
			File: &swarm.ConfigReferenceFileTarget{
				Name: ref.file(defaultConfigDir, cfg.Spec.Name),
				UID:  "0",
				GID:  "0",
				Mode: defaultAttachmentMode,
			},
		})
	}

	updated, err := writeClient.UpdateServiceConfigs(ctx, id, resolved)
	if err != nil {
		return "", fmt.Errorf("update service configs: %w", err)
	}

	return marshalResult(serviceUpdate(sectionConfigs, updated))
}

// mountValue is one entry of the mounts array, a wire shape of its own rather
// than mount.Mount: that struct carries five nested option blocks a service
// editor does not need, all of which would land in the input schema a model
// reads before every call.
type mountValue struct {
	// Type is one of volume, bind or tmpfs, validated by name here so an
	// unknown one is refused before a rolling deploy is requested.
	Type string `json:"type"`

	// Source is the volume name for a volume mount and the host path for a
	// bind. A tmpfs has none: it is memory.
	Source string `json:"source,omitempty"`

	Target   string `json:"target"`
	ReadOnly bool   `json:"readOnly,omitempty"`
}

// mountTypes are the three Swarm supports, named here so the error can list
// them. Docker's own rejection of a bad type does not.
var mountTypes = []mount.Type{mount.TypeVolume, mount.TypeBind, mount.TypeTmpfs}

// toMount converts the wire shape, refusing what Docker would refuse later.
func (v mountValue) toMount(index int) (mount.Mount, error) {
	if v.Target == "" {
		return mount.Mount{}, fmt.Errorf("mounts[%d]: target is required", index)
	}

	if !slices.Contains(mountTypes, mount.Type(v.Type)) {
		return mount.Mount{}, fmt.Errorf(
			"mounts[%d]: %q is not a mount type; expected one of %v",
			index, v.Type, mountTypes,
		)
	}

	return mount.Mount{
		Type:     mount.Type(v.Type),
		Source:   v.Source,
		Target:   v.Target,
		ReadOnly: v.ReadOnly,
	}, nil
}

// toolUpdateServiceMounts replaces the set of filesystem mounts a service's
// containers receive. Configuration level with the other two attachment
// editors, matching the REST route. The warning that a bind mount of the
// socket is a root shell lives in the tool's description, where a model reads it.
func (s *Server) toolUpdateServiceMounts(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}

	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}

	writeClient, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}

	var values []mountValue
	if err := decodeArgInto(req, "mounts", &values); err != nil {
		return "", err
	}

	// Every entry is converted before any of them is written, so a list with
	// one bad mount in it does not half-apply.
	mounts := make([]mount.Mount, 0, len(values))

	for i, value := range values {
		converted, convErr := value.toMount(i)
		if convErr != nil {
			return "", convErr
		}

		mounts = append(mounts, converted)
	}

	updated, err := writeClient.UpdateServiceMounts(ctx, id, mounts)
	if err != nil {
		return "", fmt.Errorf("update service mounts: %w", err)
	}

	return marshalResult(serviceUpdate(sectionMounts, updated))
}
