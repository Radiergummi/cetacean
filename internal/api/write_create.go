package api

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	cerrdefs "github.com/containerd/errdefs"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/api/problem"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// createDataResourceSpec describes how to create a data resource (config or
// secret). Used by handleCreateDataResource to eliminate per-resource
// boilerplate.
type createDataResourceSpec struct {
	resource     string // "config" or "secret"
	nameErrCode  string // validation error code
	conflictCode string // create-conflict error code
	basePath     string // URL path prefix including trailing slash
	typeName     string // JSON-LD type name

	acl *acl.Evaluator

	// entries completes a secret entry an MCP caller opened; nil for a type
	// that has none.
	entries *cluster.SecretEntries

	create func(
		ctx context.Context,
		name string,
		labels map[string]string,
		data []byte,
	) (string, error)
	buildFallback func(id, name string, labels map[string]string) any
	buildResponse func(id string) (any, bool)
}

func handleCreateDataResource(w http.ResponseWriter, r *http.Request, spec createDataResourceSpec) {
	req, ok := decodeJSON[createResourceRequest](w, r)
	if !ok {
		return
	}

	identity := auth.IdentityFromContext(r.Context())

	var labels map[string]string

	if req.Elicitation != "" {
		entry, ok := openEntry(w, r, spec, req)
		if !ok {
			return
		}

		req.Name, labels = entry.Name, entry.Labels
	}

	if strings.TrimSpace(req.Name) == "" {
		problem.Write(w, r, spec.nameErrCode, "name is required")
		return
	}

	if !spec.acl.Can(identity, "write", spec.resource+":"+req.Name) {
		problem.Write(w, r, "ACL002", "write access denied")
		return
	}

	if err := cluster.CheckStackLabel(nil, labels, func(resource string) bool {
		return spec.acl.Can(identity, "write", resource)
	}); err != nil {
		problem.Write(w, r, "ACL002", err.Error())
		return
	}

	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		problem.Write(w, r, spec.nameErrCode, "data must be valid base64")
		return
	}

	slog.Info("creating "+spec.resource, "name", req.Name)

	id, err := spec.create(r.Context(), req.Name, labels, data)
	if err != nil {
		if cerrdefs.IsConflict(err) {
			problem.Write(w, r, spec.conflictCode, err.Error())
			return
		}

		writeDockerError(w, r, err, spec.resource, req.Name)
		return
	}

	if req.Elicitation != "" {
		// The secret exists either way; a failure here only leaves the agent
		// asking again, so it is logged rather than turned into an error.
		if err := spec.entries.Complete(req.Elicitation, entrySubject(identity), id); err != nil {
			slog.Warn("secret created but its entry could not be completed",
				"secret", req.Name, "error", err)
		}
	}

	w.Header().Set("Location", absPath(r.Context(), spec.basePath+id))

	if preferMinimal(r) {
		writePreferCreated(w)
		return
	}

	resp, ok := spec.buildResponse(id)
	if !ok {
		resp = spec.buildFallback(id, req.Name, labels)
	}

	writeJSONStatus(
		w,
		http.StatusCreated,
		NewDetailResponse(r.Context(), spec.basePath+id, spec.typeName, resp),
	)
}

// openEntry resolves the secret entry a create names for the caller, answering
// the request itself when it can't be used. The entry's name is the one
// created: the page shows it, and a body naming another is refused.
func openEntry(
	w http.ResponseWriter,
	r *http.Request,
	spec createDataResourceSpec,
	req createResourceRequest,
) (cluster.SecretEntry, bool) {
	if spec.entries == nil {
		problem.Write(w, r, spec.nameErrCode, "only a secret is created from an elicitation")
		return cluster.SecretEntry{}, false
	}

	entry, ok := lookupEntry(w, r, spec.entries, req.Elicitation)
	if !ok {
		return cluster.SecretEntry{}, false
	}

	if req.Name != "" && req.Name != entry.Name {
		problem.Write(w, r, spec.nameErrCode,
			"name must be the one the elicitation asked for, "+entry.Name)
		return cluster.SecretEntry{}, false
	}

	return entry, true
}

// lookupEntry finds entry id for the caller, answering 404 when there is none
// and 403 when another person's MCP client opened it.
func lookupEntry(
	w http.ResponseWriter,
	r *http.Request,
	entries *cluster.SecretEntries,
	id string,
) (cluster.SecretEntry, bool) {
	if entries == nil {
		problem.Write(w, r, "SEC006", "no such secret entry")
		return cluster.SecretEntry{}, false
	}

	entry, err := entries.For(id, entrySubject(auth.IdentityFromContext(r.Context())))

	switch {
	case errors.Is(err, cluster.ErrSecretEntrySubject):
		problem.Write(w, r, "SEC007",
			"this secret entry was opened by someone else's MCP client")
		return cluster.SecretEntry{}, false
	case err != nil:
		problem.Write(w, r, "SEC006", "no such secret entry, or it has expired")
		return cluster.SecretEntry{}, false
	}

	return entry, true
}

func entrySubject(identity *auth.Identity) string {
	if identity == nil {
		return ""
	}

	return identity.Subject
}
