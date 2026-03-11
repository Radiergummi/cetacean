package api

import "net/http"

const apiPlaygroundHTML = `<!DOCTYPE html>
<html>
<head><title>Cetacean API</title><meta charset="utf-8"/></head>
<body>
  <script id="api-reference" data-url="/api.json"></script>
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
</body>
</html>`

const apiPlaceholderYAML = "openapi: '3.1.0'\ninfo:\n  title: Cetacean API\n  version: '1.0'\npaths: {}\n"

// HandleAPIDoc serves the API documentation. HTML requests get the Scalar
// playground; everything else gets the OpenAPI spec (placeholder for now).
func HandleAPIDoc(w http.ResponseWriter, r *http.Request) {
	ct := ContentTypeFromContext(r.Context())
	switch ct {
	case ContentTypeHTML:
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write([]byte(apiPlaygroundHTML)) //nolint:errcheck
	default:
		w.Header().Set("Content-Type", "application/yaml")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write([]byte(apiPlaceholderYAML)) //nolint:errcheck
	}
}
