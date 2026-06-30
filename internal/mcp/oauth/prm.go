package oauth

import (
	"encoding/json"
	"net/http"
)

// protectedResourceMetadata is the RFC 9728 Protected Resource Metadata document.
type protectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
	ResourceDocumentation  string   `json:"resource_documentation,omitempty"`
}

// HandleProtectedResourceMetadata serves the RFC 9728 protected resource
// metadata document at GET {base}/.well-known/oauth-protected-resource.
func (s *Server) HandleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	doc := protectedResourceMetadata{
		Resource:               s.cfg.MCPResource,
		AuthorizationServers:   []string{s.cfg.issuerID()},
		BearerMethodsSupported: []string{"header"},
	}
	if s.cfg.issuerID() != "" {
		doc.ResourceDocumentation = s.cfg.issuerID() + "/api"
	}

	// Marshal first so an encoding failure doesn't write partial headers
	// followed by a 500 status.
	body, err := json.Marshal(doc)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "max-age=3600")
	_, _ = w.Write(body)
}
