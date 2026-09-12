package api

import (
	"encoding/xml"
	"net/http"
)

const (
	openSearchPath      = "/opensearch.xml"
	openSearchMediaType = "application/opensearchdescription+xml"

	// OpenSearch 1.1 is a community spec, not an RFC:
	// https://github.com/dewitt/opensearch/blob/master/opensearch-1-1-draft-6.md
	openSearchNamespace = "http://a9.com/-/spec/opensearch/1.1/"
)

type openSearchDescription struct {
	XMLName xml.Name `xml:"OpenSearchDescription"`
	XMLNS   string   `xml:"xmlns,attr"`

	// ShortName is capped at 16 characters by the spec.
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

// HandleOpenSearch serves the description document that lets a browser search
// the cluster from the address bar. Templates are absolute because a URL
// template is used with no document to resolve against, which is why this is
// served rather than shipped as a static file like the web app manifest.
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
			// Extension suffixes rather than an Accept header: a template
			// carries no headers.
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
