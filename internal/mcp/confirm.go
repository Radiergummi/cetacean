package mcp

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/radiergummi/cetacean/internal/auth"
)

// confirmationTTL is how long a person has to answer before the tool asks again.
const confirmationTTL = 5 * time.Minute

// confirmField is the one property of a confirmation form.
const confirmField = "confirm"

// stateSigner signs the requestState of a multi round-trip call. Its key is
// generated at startup and never leaves memory, so a restart only means
// asking again; it is not the OAuth signer, whose key must outlive restarts.
type stateSigner struct {
	key []byte
	now func() time.Time
}

func newStateSigner() (*stateSigner, error) {
	key := make([]byte, sha256.Size)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}

	return &stateSigner{key: key, now: time.Now}, nil
}

// roundTripState is what a requestState binds. Expires is excluded from the
// match; every other field must equal what the retried call recomputes.
type roundTripState struct {
	Subject  string `json:"s,omitempty"`
	Provider string `json:"p,omitempty"`
	Tool     string `json:"t"`
	Args     string `json:"a"`
	Version  uint64 `json:"v,omitempty"`
	Bind     string `json:"b,omitempty"`
	Expires  int64  `json:"e"`
}

func (s *stateSigner) mac(payload []byte) []byte {
	h := hmac.New(sha256.New, s.key)
	h.Write(payload)

	return h.Sum(nil)
}

// sign issues a state for want, expiring confirmationTTL from now.
func (s *stateSigner) sign(want roundTripState) string {
	want.Expires = s.now().Add(confirmationTTL).Unix()

	payload, _ := json.Marshal(want) //nolint:errchkjson // a struct of strings and ints

	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(s.mac(payload))
}

// verify reports whether token is a state this process signed for exactly
// want, and has not expired.
func (s *stateSigner) verify(token string, want roundTripState) bool {
	encoded, signature, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}

	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return false
	}

	mac, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(mac, s.mac(payload)) {
		return false
	}

	var got roundTripState
	if err := json.Unmarshal(payload, &got); err != nil {
		return false
	}

	if s.now().Unix() >= got.Expires {
		return false
	}

	got.Expires, want.Expires = 0, 0

	return got == want
}

// argumentsDigest hashes a call's arguments. encoding/json writes map keys
// sorted, so two calls with the same arguments hash alike in any key order.
func argumentsDigest(req mcplib.CallToolRequest) string {
	encoded, err := json.Marshal(req.GetArguments())
	if err != nil {
		encoded = nil
	}

	sum := sha256.Sum256(encoded)

	return hex.EncodeToString(sum[:])
}

// clientElicits reports whether this request's client can answer an
// elicitation in mode. A legacy request only can through a session that
// carries server-initiated requests, which a stateless transport never has.
func clientElicits(ctx context.Context, mode string) bool {
	if info := mcpserver.RequestProtocolInfoFromContext(ctx); info != nil && info.Modern {
		return info.ClientCapabilities != nil &&
			elicitationSupports(info.ClientCapabilities.Elicitation, mode)
	}

	session := mcpserver.ClientSessionFromContext(ctx)
	if session == nil || session.SessionID() == "" {
		return false
	}

	if _, ok := session.(mcpserver.SessionWithElicitation); !ok {
		return false
	}

	withInfo, ok := session.(mcpserver.SessionWithClientInfo)
	if !ok {
		return false
	}

	return elicitationSupports(withInfo.GetClientCapabilities().Elicitation, mode)
}

// elicitationSupports reads a declared capability: an empty object means form
// mode alone, as it did before modes existed.
func elicitationSupports(capability *mcplib.ElicitationCapability, mode string) bool {
	if capability == nil {
		return false
	}

	if mode == mcplib.ElicitationModeURL {
		return capability.URL != nil
	}

	return capability.Form != nil || capability.URL == nil
}

// toolOutcome is a result a handler returns in place of its own: a request
// for input, or a person's refusal. It rides the error path only because the
// handler signature has no other; registerTools unwraps it.
type toolOutcome struct {
	result *mcplib.CallToolResult
}

func (o *toolOutcome) Error() string {
	if o.result != nil && o.result.NeedsInput() {
		return "input required"
	}

	return "declined"
}

// confirmation is the question a tool puts to a person before it writes.
type confirmation struct {
	target  string
	message string
	version uint64
	bind    string

	// declined is the structured content a refusal returns: the tool's output
	// schema still holds when nothing was written.
	declined any
}

// confirm asks a person to approve the write req describes, when the client
// can ask; build is called only then. It returns whether a person approved,
// or a *toolOutcome the handler returns as it stands.
func (s *Server) confirm(
	ctx context.Context,
	req mcplib.CallToolRequest,
	build func() (confirmation, error),
) (bool, error) {
	if !clientElicits(ctx, mcplib.ElicitationModeForm) {
		if s.config.RequireConfirmation {
			return false, fmt.Errorf(
				"%s needs a person to confirm it, and this client declares no form "+
					"elicitation to ask one (mcp.require_confirmation is on)",
				req.Params.Name,
			)
		}

		return false, nil
	}

	question, err := build()
	if err != nil {
		return false, err
	}

	tool := req.Params.Name
	want := s.roundTripState(ctx, req, question.version, question.bind)

	answer := mcpserver.ElicitationResponse(req.Params.InputResponses, tool)
	if answer == nil || !s.confirmations.verify(req.Params.RequestState, want) {
		return false, s.askToConfirm(tool, question, want)
	}

	outcome := confirmationOutcome(answer)
	logConfirmation(ctx, tool, question.target, outcome)

	if outcome == outcomeAccepted {
		return true, nil
	}

	text := "Nothing was changed: the person " + outcome + " the confirmation."
	result := mcplib.NewToolResultStructured(question.declined, text)

	return false, &toolOutcome{result: result}
}

func (s *Server) roundTripState(
	ctx context.Context,
	req mcplib.CallToolRequest,
	version uint64,
	bind string,
) roundTripState {
	state := roundTripState{
		Tool:    req.Params.Name,
		Args:    argumentsDigest(req),
		Version: version,
		Bind:    bind,
	}

	if identity := auth.IdentityFromContext(ctx); identity != nil {
		state.Subject, state.Provider = identity.Subject, identity.Provider
	}

	return state
}

func (s *Server) askToConfirm(tool string, question confirmation, want roundTripState) error {
	builder := mcpserver.NewInputRequestBuilder(s.confirmations.sign(want))
	builder.Elicit(tool, mcplib.ElicitationParams{
		Mode:    mcplib.ElicitationModeForm,
		Message: question.message,
		RequestedSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				confirmField: map[string]any{
					"type":        "boolean",
					"title":       "Confirm",
					"description": "Go ahead with this change.",
				},
			},
			"required": []string{confirmField},
		},
	})

	return &toolOutcome{result: builder.ToolResult()}
}

const (
	outcomeAccepted  = "accepted"
	outcomeDeclined  = "declined"
	outcomeCancelled = "cancelled"
)

// confirmationOutcome accepts only an explicit true; an accepted form that
// says anything else is a refusal.
func confirmationOutcome(answer *mcplib.ElicitationResult) string {
	switch answer.Action {
	case mcplib.ElicitationResponseActionAccept:
		content, _ := answer.Content.(map[string]any)
		if confirmed, _ := content[confirmField].(bool); confirmed {
			return outcomeAccepted
		}

		return outcomeDeclined
	case mcplib.ElicitationResponseActionCancel:
		return outcomeCancelled
	default:
		return outcomeDeclined
	}
}

// logConfirmation records a person's answer. The change history has no actor
// yet (#438), so this line is the only record that a person confirmed.
func logConfirmation(ctx context.Context, tool, target, outcome string) {
	slog.Info("MCP confirmation", append(callerAttrs(ctx),
		"tool", tool,
		"target", target,
		"outcome", outcome,
	)...)
}

// logConfirmableWrite is the line an eliciting tool writes once it has acted.
func logConfirmableWrite(ctx context.Context, tool, target string, confirmed bool) {
	slog.Info("MCP write", append(callerAttrs(ctx),
		"tool", tool,
		"target", target,
		"confirmed", confirmed,
	)...)
}

func callerAttrs(ctx context.Context) []any {
	identity := auth.IdentityFromContext(ctx)
	if identity == nil {
		return []any{"subject", ""}
	}

	client, _ := identity.Raw[auth.RawTokenClient].(string)

	return []any{
		"subject", identity.Subject,
		"provider", identity.Provider,
		"client", client,
	}
}

// asToolOutcome extracts the result a handler handed back through its error.
func asToolOutcome(err error) (*mcplib.CallToolResult, bool) {
	outcome, ok := errors.AsType[*toolOutcome](err)
	if !ok {
		return nil, false
	}

	return outcome.result, true
}
