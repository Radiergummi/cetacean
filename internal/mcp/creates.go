package mcp

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// createResult is what a create answers with: enough to reference the new
// resource on the next call, and nothing more.
//
// A secret's payload is deliberately absent. The caller supplied it, so
// repeating it adds nothing and puts a credential in a transcript that may be
// logged, replayed to a model, or shown to someone reviewing the session.
type createResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// decodePayload reads the `data` argument, honouring an explicit `encoding`.
//
// Base64 exists because a config is very often a file — a TLS certificate, an
// nginx block, a key — and JSON string escaping mangles one. The encoding is
// declared rather than guessed from the shape of the string: plenty of ordinary
// passwords are valid base64, and silently decoding one writes a secret whose
// value is not the value. Nothing detects that until an authentication fails at
// runtime, far from the call that caused it.
func decodePayload(req mcplib.CallToolRequest) ([]byte, error) {
	raw := req.GetString("data", "")
	if raw == "" {
		return nil, fmt.Errorf("data: required, and may not be empty")
	}

	switch encoding := req.GetString("encoding", "utf8"); encoding {
	case "utf8":
		return []byte(raw), nil

	case "base64":
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf(
				"data: encoding was given as base64, but the value does not decode: %w",
				err,
			)
		}

		return decoded, nil

	default:
		return nil, fmt.Errorf("encoding %q: expected \"utf8\" or \"base64\"", encoding)
	}
}

// createLabels reads the optional `labels` argument.
func createLabels(req mcplib.CallToolRequest) (map[string]string, error) {
	if _, present := req.GetArguments()["labels"]; !present {
		return nil, nil
	}

	var labels map[string]string
	if err := decodeArgInto(req, "labels", &labels); err != nil {
		return nil, err
	}

	return labels, nil
}

// toolCreateSecret creates a Swarm secret.
//
// Swarm secrets are immutable, so "rotate this password" is three calls in
// order: create the replacement, repoint every service that uses it, remove the
// old one. Cetacean could previously only do the third, which made the whole
// sequence impossible rather than merely awkward — this is the first.
//
// The ACL key is the secret's name, matching every other secret write and the
// REST route: there is no ID yet to key on, which is the one respect in which a
// create differs from the edits around it.
func (s *Server) toolCreateSecret(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return "", err
	}

	// Before the payload is even decoded, for the same reason every other
	// mutation checks first: a refusal must not depend on the value being
	// well-formed, or on Docker being reachable.
	if err := s.checkWrite(ctx, "secret", name); err != nil {
		return "", err
	}

	writeClient, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}

	data, err := decodePayload(req)
	if err != nil {
		return "", err
	}

	labels, err := createLabels(req)
	if err != nil {
		return "", err
	}

	id, err := writeClient.CreateSecret(ctx, swarm.SecretSpec{
		Annotations: swarm.Annotations{Name: name, Labels: labels},
		Data:        data,
	})
	if err != nil {
		return "", fmt.Errorf("create secret: %w", err)
	}

	// Seeded so the very next call can use it. Both tools tell a caller to
	// create the replacement and then repoint the service, and resolution
	// reads the cache — which the watcher fills asynchronously, a few hundred
	// milliseconds later. Against a live cluster that made the documented
	// sequence fail on its second step with "no such secret", for a secret
	// whose ID had just been returned.
	//
	// Data is deliberately zeroed: the cache backs every listing, and a
	// secret's value has no business being in it. The watcher's own record
	// arrives moments later and carries none either.
	s.cache.SetSecret(swarm.Secret{
		ID: id,
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{Name: name, Labels: labels},
		},
	})

	return marshalResult(createResult{ID: id, Name: name, Type: "secret"})
}

// toolCreateConfig creates a Swarm config.
//
// Unlike a secret, a config's content can be read back afterwards. It is still
// not echoed here: the caller already has it, describe will serve it, and the
// two creates answering in the same shape is worth more than the convenience.
func (s *Server) toolCreateConfig(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return "", err
	}

	if err := s.checkWrite(ctx, "config", name); err != nil {
		return "", err
	}

	writeClient, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}

	data, err := decodePayload(req)
	if err != nil {
		return "", err
	}

	labels, err := createLabels(req)
	if err != nil {
		return "", err
	}

	id, err := writeClient.CreateConfig(ctx, swarm.ConfigSpec{
		Annotations: swarm.Annotations{Name: name, Labels: labels},
		Data:        data,
	})
	if err != nil {
		return "", fmt.Errorf("create config: %w", err)
	}

	// Seeded for the same reason create_secret seeds: the next call in the
	// sequence resolves by name against the cache. The content is left out —
	// the watcher brings it, and nothing between here and then needs it.
	s.cache.SetConfig(swarm.Config{
		ID: id,
		Spec: swarm.ConfigSpec{
			Annotations: swarm.Annotations{Name: name, Labels: labels},
		},
	})

	return marshalResult(createResult{ID: id, Name: name, Type: "config"})
}
