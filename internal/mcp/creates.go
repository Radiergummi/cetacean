package mcp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cluster"
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
		return nil, errors.New("data: required, and may not be empty")
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
	if raw, present := req.GetArguments()["labels"]; !present || raw == nil {
		return nil, nil
	}

	var labels map[string]string
	if err := decodeArgInto(req, "labels", &labels); err != nil {
		return nil, err
	}

	return labels, nil
}

// dataResourceSpec is the half of a create that differs between a secret and a
// config: what to call it and which use case writes it. The other half is
// identical, and includes the ordering the refusal path depends on.
type dataResourceSpec struct {
	// kind is both the ACL resource type and the result's Type.
	kind string

	// create also seeds the record into the cache, which resolution reads and
	// the watcher fills only later: the documented rotation sequence would
	// otherwise fail its second step with "no such secret".
	create func(
		w *cluster.Writes,
		ctx context.Context,
		c cluster.Caller,
		name string,
		labels map[string]string,
		data []byte,
	) (string, error)

	// entersValue lets a person enter the value in the dashboard instead.
	entersValue bool
}

// Swarm secrets and configs are immutable, so "rotate this password" is three
// calls in order: create the replacement, repoint every service using it,
// remove the old one. The ACL key is the resource's name, matching the REST
// route — there is no ID yet, which is the one way a create differs.
var (
	secretResource = dataResourceSpec{
		kind:        "secret",
		entersValue: true,
		create:      (*cluster.Writes).CreateSecret,
	}

	configResource = dataResourceSpec{
		kind:   "config",
		create: (*cluster.Writes).CreateConfig,
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

		labels, err := createLabels(req)
		if err != nil {
			return "", err
		}

		// Early too: a person sent to enter the value must not be refused
		// only after typing it.
		if err := cluster.CheckStackLabel(nil, labels, s.writePredicate(ctx)); err != nil {
			return "", err
		}

		if spec.entersValue {
			if err := s.checkInlineValue(ctx, req); err != nil {
				return "", err
			}

			if req.GetString("data", "") == "" {
				return s.enterSecretValue(ctx, req, name, labels)
			}
		}

		if _, err := s.requireWriteClient(); err != nil {
			return "", err
		}

		data, err := decodePayload(req)
		if err != nil {
			return "", err
		}

		id, err := spec.create(s.writes, ctx, s.writeCaller(ctx), name, labels, data)
		if err != nil {
			return "", fmt.Errorf(
				"create %s: %w",
				spec.kind,
				writeFailure(ctx, err, spec.kind, name),
			)
		}

		return marshalResult(createResult{ID: id, Name: name, Type: spec.kind})
	}
}
