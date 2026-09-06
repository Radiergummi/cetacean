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

// dataResourceSpec is the half of a create that differs between a secret and a
// config: what to call the thing, how to write it, and how to seed it. The
// other half — require a name, check the write grant before anything else,
// require a write client, decode the payload, read the labels — is identical,
// and that half includes the ordering the refusal path depends on.
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

	// seed writes the created record into the cache. Both tools tell a caller
	// to create the replacement and then repoint the service, and resolution
	// reads the cache — which the watcher fills asynchronously, a few hundred
	// milliseconds later. Against a live cluster that made the documented
	// sequence fail on its second step with "no such secret", for a resource
	// whose ID had just been returned.
	//
	// Neither seed carries the payload. For a secret that is the point: the
	// cache backs every listing and a credential has no business in it, and
	// the watcher's own record carries none either. For a config it is merely
	// unnecessary — the watcher brings the content moments later and nothing
	// in between reads it.
	seed func(c *cache.Cache, id, name string, labels map[string]string)
}

// Swarm secrets and configs are immutable, so "rotate this password" is three
// calls in order: create the replacement, repoint every service that uses it,
// remove the old one. Cetacean could previously only do the third, which made
// the whole sequence impossible rather than merely awkward.
//
// The ACL key is the resource's name, matching every other write of its type
// and the REST route: there is no ID yet to key on, which is the one respect in
// which a create differs from the edits around it.
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
