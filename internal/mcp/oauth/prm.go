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
		AuthorizationServers:   []string{s.cfg.Issuer},
		BearerMethodsSupported: []string{"header"},
	}
	if s.cfg.BasePath != "" || s.cfg.Issuer != "" {
		doc.ResourceDocumentation = s.cfg.Issuer + s.cfg.BasePath + "/api"
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "max-age=3600")
	if err := json.NewEncoder(w).Encode(doc); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
