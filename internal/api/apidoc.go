package api

import "net/http"

const apiPlaygroundHTML = `<!DOCTYPE html>
<html>
<head><title>Cetacean API</title><meta charset="utf-8"/></head>
<body>
  <script id="api-reference" data-url="/api"></script>
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
</body>
</html>`

// HandleAPIDoc serves the API documentation. HTML requests get the Scalar
// playground; everything else gets the OpenAPI spec.
func HandleAPIDoc(spec []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ct := ContentTypeFromContext(r.Context())
		switch ct {
		case ContentTypeHTML:
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.Write([]byte(apiPlaygroundHTML)) //nolint:errcheck
		default:
			w.Header().Set("Content-Type", "application/yaml")
			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.Write(spec) //nolint:errcheck
		}
	}
}
