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
	"maps"
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

func newStateSigner() *stateSigner {
	key := make([]byte, sha256.Size)
	_, _ = rand.Read(key)

	return &stateSigner{key: key, now: time.Now}
}

// roundTripState is what a requestState binds: the call (caller, tool and
// arguments), the question it was issued with, and the choices a person made
// in earlier rounds of the same call, which a later round must not lose.
type roundTripState struct {
	Subject  string            `json:"s,omitempty"`
	Provider string            `json:"p,omitempty"`
	Tool     string            `json:"t"`
	Args     string            `json:"a"`
	Asked    string            `json:"q,omitempty"`
	Version  uint64            `json:"v,omitempty"`
	Bind     string            `json:"b,omitempty"`
	Choices  map[string]choice `json:"c,omitempty"`
	Expires  int64             `json:"e"`
}

// choice is a person's answer to a choose question, valid while the options
// it was chosen from, which Bind identifies, still stand.
type choice struct {
	Bind  string `json:"b"`
	Value string `json:"v"`
}

// sameCall reports whether two states belong to the same call.
func (st roundTripState) sameCall(other roundTripState) bool {
	return st.Subject == other.Subject && st.Provider == other.Provider &&
		st.Tool == other.Tool && st.Args == other.Args
}

// asked reports whether the state was issued with exactly this question.
func (st roundTripState) asked(key string, version uint64, bind string) bool {
	return st.Asked == key && st.Version == version && st.Bind == bind
}

func (s *stateSigner) mac(payload []byte) []byte {
	h := hmac.New(sha256.New, s.key)
	h.Write(payload)

	return h.Sum(nil)
}

// sign issues state, expiring ttl from now.
func (s *stateSigner) sign(state roundTripState, ttl time.Duration) string {
	state.Expires = s.now().Add(ttl).Unix()

	payload, _ := json.Marshal(state) //nolint:errchkjson // strings, ints and a map of them

	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(s.mac(payload))
}

// open returns the state token carries, if this process signed it and it has
// not expired.
func (s *stateSigner) open(token string) (roundTripState, bool) {
	encoded, signature, ok := strings.Cut(token, ".")
	if !ok {
		return roundTripState{}, false
	}

	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return roundTripState{}, false
	}

	mac, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(mac, s.mac(payload)) {
		return roundTripState{}, false
	}

	var state roundTripState
	if err := json.Unmarshal(payload, &state); err != nil {
		return roundTripState{}, false
	}

	if s.now().Unix() >= state.Expires {
		return roundTripState{}, false
	}

	return state, true
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
	prior, valid := s.priorRound(ctx, req)

	answer := mcpserver.ElicitationResponse(req.Params.InputResponses, tool)
	if answer == nil || !valid || !prior.asked(tool, question.version, question.bind) {
		state := s.nextRound(ctx, req, tool, question.version, question.bind)

		return false, askToConfirm(tool, question, state)
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

// callState is the part of a state that names the call req makes.
func callState(ctx context.Context, req mcplib.CallToolRequest) roundTripState {
	state := roundTripState{Tool: req.Params.Name, Args: argumentsDigest(req)}

	if identity := auth.IdentityFromContext(ctx); identity != nil {
		state.Subject, state.Provider = identity.Subject, identity.Provider
	}

	return state
}

// priorRound returns the state req echoes, when it is valid and was issued
// for this same call; anything else is no state at all.
func (s *Server) priorRound(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (roundTripState, bool) {
	state, ok := s.confirmations.open(req.Params.RequestState)
	if !ok || !state.sameCall(callState(ctx, req)) {
		return roundTripState{}, false
	}

	return state, true
}

// nextRound signs the state for asking key, carrying every choice made so
// far, including one answered by this very request.
func (s *Server) nextRound(
	ctx context.Context,
	req mcplib.CallToolRequest,
	key string,
	version uint64,
	bind string,
) string {
	return s.nextRoundLasting(ctx, req, key, version, bind, confirmationTTL)
}

// nextRoundLasting is nextRound for a question that takes longer to answer.
func (s *Server) nextRoundLasting(
	ctx context.Context,
	req mcplib.CallToolRequest,
	key string,
	version uint64,
	bind string,
	ttl time.Duration,
) string {
	next := callState(ctx, req)
	next.Asked, next.Version, next.Bind = key, version, bind

	if prior, ok := s.priorRound(ctx, req); ok {
		next.Choices = carriedChoices(prior, req.Params.InputResponses)
	}

	return s.confirmations.sign(next, ttl)
}

func carriedChoices(prior roundTripState, responses mcplib.InputResponses) map[string]choice {
	choices := maps.Clone(prior.Choices)

	answer := mcpserver.ElicitationResponse(responses, prior.Asked)
	if prior.Asked == "" || answer == nil ||
		answer.Action != mcplib.ElicitationResponseActionAccept {
		return choices
	}

	content, _ := answer.Content.(map[string]any)
	if value, ok := content[prior.Asked].(string); ok {
		if choices == nil {
			choices = map[string]choice{}
		}

		choices[prior.Asked] = choice{Bind: prior.Bind, Value: value}
	}

	return choices
}

func askToConfirm(tool string, question confirmation, state string) error {
	builder := mcpserver.NewInputRequestBuilder(state)
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
	slog.InfoContext(ctx, "MCP confirmation", append(callerAttrs(ctx),
		"tool", tool,
		"target", target,
		"outcome", outcome,
	)...)
}

// logConfirmableWrite is the line an eliciting tool writes once it has acted.
func logConfirmableWrite(ctx context.Context, tool, target string, confirmed bool) {
	slog.InfoContext(ctx, "MCP write", append(callerAttrs(ctx),
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

	return []any{
		"subject", identity.Subject,
		"provider", identity.Provider,
		"client", identity.ClientID,
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
