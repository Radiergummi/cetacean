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
	}
	if iss != "" {
		doc.ResourceDocumentation = iss + "/api"
	}

	return func(w http.ResponseWriter, _ *http.Request) {
		writeDiscoveryDoc(w, doc, "application/json")
	}
}
