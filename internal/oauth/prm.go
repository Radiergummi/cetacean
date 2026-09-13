package oauth

import (
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
	iss := s.cfg.issuerID()
	doc := protectedResourceMetadata{
		Resource:               s.cfg.MCPResource,
		AuthorizationServers:   []string{iss},
		BearerMethodsSupported: []string{"header"},
	}
	if iss != "" {
		doc.ResourceDocumentation = iss + "/api"
	}

	writeDiscoveryDoc(w, doc, "application/json")
}
