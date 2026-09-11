package api

import (
	"encoding/xml"
	"net/http"
)

// openSearchPath is where the description document lives, and the href
// frontend/index.html advertises it at.
const openSearchPath = "/opensearch.xml"

// openSearchMediaType is the type registered for an OpenSearch description
// document. A browser will not adopt one served as anything else.
const openSearchMediaType = "application/opensearchdescription+xml"

// openSearchNamespace is the OpenSearch 1.1 namespace. The specification is a
// community document rather than an RFC — there is none to cite, and looking
// for one is a waste of an afternoon.
//
// Specification: https://github.com/dewitt/opensearch/blob/master/opensearch-1-1-draft-6.md
const openSearchNamespace = "http://a9.com/-/spec/opensearch/1.1/"

// openSearchDescription is the description document. Field order is the
// document order the encoder emits, and the specification's own examples use
// it.
type openSearchDescription struct {
	XMLName xml.Name `xml:"OpenSearchDescription"`
	XMLNS   string   `xml:"xmlns,attr"`

	// ShortName is capped at 16 characters by the specification, and a
	// browser renders it as the name of the search engine.
	ShortName string `xml:"ShortName"`

	Description   string          `xml:"Description"`
	InputEncoding string          `xml:"InputEncoding"`
	Image         openSearchIcon  `xml:"Image"`
	URLs          []openSearchURL `xml:"Url"`
}

type openSearchIcon struct {
	Width  int    `xml:"width,attr"`
	Height int    `xml:"height,attr"`
	Type   string `xml:"type,attr"`
	URL    string `xml:",chardata"`
}

type openSearchURL struct {
	Type     string `xml:"type,attr"`
	Template string `xml:"template,attr"`
}

// HandleOpenSearch serves the OpenSearch description document, which is what
// lets a browser offer the cluster's own search from the address bar.
//
// The templates are absolute because a URL template is used on its own, with
// no document to resolve against — which is the whole reason this document is
// served by the server rather than sitting in frontend/public beside the web
// app manifest. Set server.public_url behind a proxy; without it the origin is
// taken from the request, and the trusted-proxy rules in requestOrigin decide
// how much of that is believed.
func HandleOpenSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	origin := originOf(r)

	link := func(path string) string { return origin + absPath(ctx, path) }

	search := link("/search")

	doc := openSearchDescription{
		XMLNS:     openSearchNamespace,
		ShortName: "Cetacean",
		Description: "Search services, stacks, nodes, tasks, configs, secrets, " +
			"networks and volumes in the Swarm cluster",
		InputEncoding: "UTF-8",
		Image: openSearchIcon{
			Width:  32,
			Height: 32,
			Type:   "image/png",
			URL:    link("/favicon-32x32.png"),
		},
		URLs: []openSearchURL{
			{Type: "text/html", Template: search + "?q={searchTerms}"},
			// The machine-readable pair a feed reader or a script can use,
			// both of which /search already serves — the extension suffix
			// rather than an Accept header, since a template carries no
			// headers.
			{
				Type:     "application/atom+xml",
				Template: link("/search.atom") + "?q={searchTerms}",
			},
			{
				Type:     "application/json",
				Template: link("/search.json") + "?q={searchTerms}",
			},
		},
	}

	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		writeErrorCode(w, r, "API009", "failed to serialize response")

		return
	}

	body = append([]byte(xml.Header), body...)

	w.Header().Set("Content-Type", openSearchMediaType)
	writeRawWithETag(w, r, body)
}
