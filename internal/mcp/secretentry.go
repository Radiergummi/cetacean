package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// canEnterSecrets reports whether a person can be sent to the dashboard to
// enter a secret for this caller. The dashboard completes an entry only for
// the subject that opened it, so a caller without a token's subject can't.
func (s *Server) canEnterSecrets(ctx context.Context) bool {
	return s.secretEntries != nil && s.dashboardURL != "" &&
		auth.IdentityFromContext(ctx).FromToken() &&
		clientElicits(ctx, mcplib.ElicitationModeURL)
}

// checkInlineValue refuses a value passed as an argument where it should not
// travel through the model: from a client that has the dashboard path, or
// from any client when the operator turned inline values off.
func (s *Server) checkInlineValue(ctx context.Context, req mcplib.CallToolRequest) error {
	if req.GetString("data", "") == "" {
		return nil
	}

	if s.canEnterSecrets(ctx) {
		return errors.New(
			"data: leave it out; this client can send the person to Cetacean's dashboard " +
				"to enter the value, which keeps it out of the conversation",
		)
	}

	if !s.config.InlineSecretValues {
		return errors.New(
			"data: this server accepts no secret value as an argument " +
				"(mcp.inline_secret_values is off); use a client that supports URL elicitation",
		)
	}

	return nil
}

// enterSecretValue has a person enter the value of secret name in the
// dashboard, and answers once they have with the secret they created.
func (s *Server) enterSecretValue(
	ctx context.Context,
	req mcplib.CallToolRequest,
	name string,
	labels map[string]string,
) (string, error) {
	encoding := req.GetString("encoding", "utf8")
	if encoding != "utf8" && encoding != "base64" {
		return "", fmt.Errorf("encoding %q: expected \"utf8\" or \"base64\"", encoding)
	}

	if !s.canEnterSecrets(ctx) {
		return "", errors.New(
			"data: required, and may not be empty; this client can't send the person " +
				"to Cetacean's dashboard to enter it",
		)
	}

	tool := req.Params.Name
	subject := auth.IdentityFromContext(ctx).Subject

	if prior, ok := s.priorRound(ctx, req); ok && prior.Asked == tool {
		entry, err := s.secretEntries.For(prior.Bind, subject)
		if err == nil {
			return s.resumeSecretEntry(ctx, req, entry)
		}
	}

	entry, err := s.secretEntries.Open(subject, name, labels, encoding)
	if err != nil {
		return "", fmt.Errorf("create_secret: %w", err)
	}

	return "", s.askForSecretValue(ctx, req, entry)
}

// resumeSecretEntry answers a retry: the secret, once the person created it;
// the person's refusal; or the same question again while they haven't.
func (s *Server) resumeSecretEntry(
	ctx context.Context,
	req mcplib.CallToolRequest,
	entry cluster.SecretEntry,
) (string, error) {
	tool := req.Params.Name

	if id := entry.SecretID(); id != "" {
		s.secretEntries.Close(entry.ID)
		logConfirmableWrite(ctx, tool, "secret:"+entry.Name, true)

		return marshalResult(createResult{ID: id, Name: entry.Name, Type: "secret"})
	}

	answer := mcpserver.ElicitationResponse(req.Params.InputResponses, tool)
	if answer != nil && answer.Action != mcplib.ElicitationResponseActionAccept {
		s.secretEntries.Close(entry.ID)

		outcome := outcomeDeclined
		if answer.Action == mcplib.ElicitationResponseActionCancel {
			outcome = outcomeCancelled
		}

		logConfirmation(ctx, tool, "secret:"+entry.Name, outcome)

		return "", &toolOutcome{result: mcplib.NewToolResultStructured(
			createResult{Name: entry.Name, Type: "secret"},
			"Nothing was created: the person "+outcome+" entering the secret's value.",
		)}
	}

	return "", s.askForSecretValue(ctx, req, entry)
}

func (s *Server) askForSecretValue(
	ctx context.Context,
	req mcplib.CallToolRequest,
	entry cluster.SecretEntry,
) error {
	tool := req.Params.Name
	state := s.nextRoundLasting(ctx, req, tool, 0, entry.ID, cluster.SecretEntryTTL)

	builder := mcpserver.NewInputRequestBuilder(state)
	builder.Elicit(tool, mcplib.ElicitationParams{
		Mode: mcplib.ElicitationModeURL,
		Message: fmt.Sprintf(
			"Enter the value of the new secret %s in Cetacean. It goes from your "+
				"browser to Cetacean, not through the agent.",
			entry.Name,
		),
		ElicitationID: entry.ID,
		URL:           s.dashboardURL + "/secrets/new?elicitation=" + url.QueryEscape(entry.ID),
	})

	return &toolOutcome{result: builder.ToolResult()}
}
