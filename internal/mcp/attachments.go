package mcp

import (
	"context"
	"fmt"
	"os"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// defaultAttachmentMode is the file mode Swarm gives a mounted secret or
// config: readable by everyone in the container, writable by no one. It
// matches what the REST handler writes, so the same attachment made through
// either transport lands identically.
const defaultAttachmentMode os.FileMode = 0o444

// attachmentRef is one secret or config a container should receive.
//
// It names the resource rather than giving its ID, because a model holds
// names: every listing returns them, every completion offers them, and a
// describe prints them. Docker's reference type wants both and rejects a pair
// that disagrees, so the ID is resolved here from the name the caller gave.
type attachmentRef struct {
	Name string `json:"name"`

	// Target is the file the resource is mounted as. A secret's target is
	// relative to /run/secrets unless it is an absolute path; a config's is
	// the path itself. Defaults to the resource's own name, which is what
	// Swarm and the REST handler both do.
	Target string `json:"target,omitempty"`
}

// file builds the mount target shared by both reference types.
func (a attachmentRef) file(resourceName string) string {
	if a.Target != "" {
		return a.Target
	}

	return resourceName
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

// toolUpdateServiceSecrets replaces the set of secrets a service receives.
//
// This is the second of the three calls a rotation needs — create the
// replacement, repoint the services, drop the old one — and the reason
// create_secret was worth adding: Swarm secrets are immutable, so there is no
// other way to change what a container is handed.
//
// It sits at the configuration level, the same one the REST route for this
// operation requires. The spec argued for raising it, on the grounds that
// handing a container a different credential is a bigger step for an agent
// acting unattended; that was rejected in favour of the operations level
// meaning one thing whichever transport an operator reaches for.
//
// The list replaces rather than merges, like every other wholesale section
// here: an empty list detaches every secret.
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
				Name: ref.file(sec.Spec.Name),
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

// toolUpdateServiceConfigs is the config counterpart.
//
// It differs from the secret tool only in the reference type and the read
// grant it checks — a config's content is readable, so attaching one discloses
// less, but the grant is still required for the same reason.
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
				Name: ref.file(cfg.Spec.Name),
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
