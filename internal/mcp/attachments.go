package mcp

import (
	"context"
	"fmt"
	"slices"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// attachmentRef is one secret or config a container should receive. It names
// the resource rather than giving its ID, because a model holds names: every
// listing, completion and describe hands one back. Docker's reference type
// wants both and rejects a disagreeing pair, so the ID is resolved here.
type attachmentRef struct {
	Name string `json:"name"`

	// Target is the file the resource is mounted as. A secret's target is
	// relative to /run/secrets unless it is an absolute path; a config's is
	// the path itself. Left empty, cluster.SecretReference and
	// cluster.ConfigReference choose it.
	Target string `json:"target,omitempty"`
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

	ctx, id, cond, err := s.pinVersion(ctx, req, "service", id)
	if err != nil {
		return "", err
	}

	if _, err := s.requireWriteClient(); err != nil {
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

		resolved = append(resolved, cluster.SecretReference(sec, ref.Target))
	}

	updated, err := s.writes.UpdateService(
		ctx, s.writeCaller(ctx), cluster.OpUpdateServiceSecrets, id, cluster.SetSecrets(resolved),
	)
	if err != nil {
		return "", fmt.Errorf("update service secrets: %w", s.refused(ctx, cond, err))
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

	ctx, id, cond, err := s.pinVersion(ctx, req, "service", id)
	if err != nil {
		return "", err
	}

	if _, err := s.requireWriteClient(); err != nil {
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

		resolved = append(resolved, cluster.ConfigReference(cfg, ref.Target))
	}

	updated, err := s.writes.UpdateService(
		ctx, s.writeCaller(ctx), cluster.OpUpdateServiceConfigs, id, cluster.SetConfigs(resolved),
	)
	if err != nil {
		return "", fmt.Errorf("update service configs: %w", s.refused(ctx, cond, err))
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
// editors, matching the REST route; a new bind mount needs the impactful level.
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

	ctx, id, cond, err := s.pinVersion(ctx, req, "service", id)
	if err != nil {
		return "", err
	}

	if _, err := s.requireWriteClient(); err != nil {
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

	updated, err := s.writes.UpdateService(
		ctx, s.writeCaller(ctx), cluster.OpUpdateServiceMounts, id, cluster.SetMounts(mounts),
	)
	if err != nil {
		return "", fmt.Errorf("update service mounts: %w", s.refused(ctx, cond, err))
	}

	return marshalResult(serviceUpdate(sectionMounts, updated))
}
