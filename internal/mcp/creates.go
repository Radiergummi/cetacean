package mcp

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cache"
)

// createResult is what a create answers with: enough to reference the new
// resource on the next call, and nothing more. A secret's payload is absent —
// the caller supplied it, and repeating it puts a credential in a transcript
// that may be logged or replayed to a model.
type createResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// decodePayload reads the `data` argument, honouring an explicit `encoding`.
// Base64 exists because a config is often a file that JSON escaping mangles.
// The encoding is declared rather than guessed: plenty of ordinary passwords
// are valid base64, and decoding one writes a secret whose value is not the value.
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

// dataResourceSpec is the half of a create that differs between a secret and a
// config: what to call it, how to write it, and how to seed it. The other half
// is identical, and includes the ordering the refusal path depends on.
type dataResourceSpec struct {
	// kind is both the ACL resource type and the result's Type.
	kind string

	create func(
		wc DockerWriteClient,
		ctx context.Context,
		name string,
		labels map[string]string,
		data []byte,
	) (string, error)

	// seed writes the created record into the cache, which resolution reads and
	// the watcher fills only a few hundred milliseconds later — otherwise the
	// documented rotation sequence fails its second step with "no such secret".
	// Neither seed carries the payload: the cache backs every listing.
	seed func(c *cache.Cache, id, name string, labels map[string]string)
}

// Swarm secrets and configs are immutable, so "rotate this password" is three
// calls in order: create the replacement, repoint every service using it,
// remove the old one. The ACL key is the resource's name, matching the REST
// route — there is no ID yet, which is the one way a create differs.
var (
	secretResource = dataResourceSpec{
		kind: "secret",
		create: func(
			wc DockerWriteClient,
			ctx context.Context,
			name string,
			labels map[string]string,
			data []byte,
		) (string, error) {
			return wc.CreateSecret(ctx, swarm.SecretSpec{
				Annotations: swarm.Annotations{Name: name, Labels: labels},
				Data:        data,
			})
		},
		seed: func(c *cache.Cache, id, name string, labels map[string]string) {
			c.SetSecret(swarm.Secret{
				ID:   id,
				Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: name, Labels: labels}},
			})
		},
	}

	configResource = dataResourceSpec{
		kind: "config",
		create: func(
			wc DockerWriteClient,
			ctx context.Context,
			name string,
			labels map[string]string,
			data []byte,
		) (string, error) {
			return wc.CreateConfig(ctx, swarm.ConfigSpec{
				Annotations: swarm.Annotations{Name: name, Labels: labels},
				Data:        data,
			})
		},
		seed: func(c *cache.Cache, id, name string, labels map[string]string) {
			c.SetConfig(swarm.Config{
				ID:   id,
				Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: name, Labels: labels}},
			})
		},
	}
)

// createHandler builds the tool handler for one data resource, the way
// removeHandler does for the five removals.
func (s *Server) createHandler(
	spec dataResourceSpec,
) func(context.Context, mcplib.CallToolRequest) (string, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return "", err
		}

		// Before the payload is even decoded, for the same reason every other
		// mutation checks first: a refusal must not depend on the value being
		// well-formed, or on Docker being reachable.
		if err := s.checkWrite(ctx, spec.kind, name); err != nil {
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

		id, err := spec.create(writeClient, ctx, name, labels, data)
		if err != nil {
			return "", fmt.Errorf("create %s: %w", spec.kind, err)
		}

		spec.seed(s.cache, id, name, labels)

		return marshalResult(createResult{ID: id, Name: name, Type: spec.kind})
	}
}
