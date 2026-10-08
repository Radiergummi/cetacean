package oauth

import (
	"net/http"
)

// protectedResourceMetadata is the RFC 9728 Protected Resource Metadata document.
type protectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`

	// Empty rather than absent, for the reason the authorization server metadata
	// says it there: this resource has no scopes, which is worth stating.
	ScopesSupported []string `json:"scopes_supported"`

	// Omitted when a resource has no name rather than sent empty: §3.2 has a
	// zero-valued parameter left out.
	ResourceName string `json:"resource_name,omitempty"`

	ResourceDocumentation string `json:"resource_documentation,omitempty"`
}

// protectedResourceMetadataHandler serves the RFC 9728 document for one
// resource. One document describes one resource, so each gets its own handler at
// its own path rather than a single document naming them all.
func (s *Server) protectedResourceMetadataHandler(r Resource) http.HandlerFunc {
	// Every field is fixed at registration, so the document is built once here
	// rather than per request.
	iss := s.cfg.issuerID()
	doc := protectedResourceMetadata{
		Resource:               s.cfg.identifierOf(r),
		AuthorizationServers:   []string{iss},
		BearerMethodsSupported: []string{"header"},
		ScopesSupported:        []string{},
		ResourceName:           r.Name,
	}
	if iss != "" {
		doc.ResourceDocumentation = iss + "/api"
	}

	return func(w http.ResponseWriter, _ *http.Request) {
		writeDiscoveryDoc(w, doc, "application/json")
	}
}
