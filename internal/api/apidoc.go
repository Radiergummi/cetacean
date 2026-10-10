package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"

	"gopkg.in/yaml.v3"
)

// apiPlaygroundHTML takes the base href; every reference below it is relative.
const apiPlaygroundHTML = `<!DOCTYPE html>
<html>
<head><title>Cetacean API</title><meta charset="utf-8"/><base href="%s"/></head>
<body>
  <script id="api-reference" data-url="api"></script>
  <script src="api/scalar.js"></script>
</body>
</html>`

// openAPIYAMLMediaType is the type OAI registered for an OpenAPI document in
// YAML; the +json suffix names the JSON form.
const (
	openAPIYAMLPath = "/api/openapi.yaml"

	openAPIYAMLMediaType = "application/vnd.oai.openapi"
)

// HandleAPIDoc returns the negotiated handler and the one behind the .yaml
// address: HTML gets the Scalar playground, */* and JSON the spec as JSON, and
// YAML the source, verbatim but for a base path in its servers. One call, so
// they share the bodies, each hashed and compressed once.
func HandleAPIDoc(specYAML []byte, basePath string) (negotiated, yamlOnly http.HandlerFunc) {
	specYAML, err := rebaseServers(specYAML, basePath)
	if err != nil {
		panic("openapi spec " + err.Error())
	}

	// Convert YAML to JSON once at startup.
	var parsed any
	if err := yaml.Unmarshal(specYAML, &parsed); err != nil {
		panic("openapi spec is not valid YAML: " + err.Error())
	}
	specJSON, err := json.Marshal(convertYAMLToJSON(parsed))
	if err != nil {
		panic("openapi spec could not be converted to JSON: " + err.Error())
	}

	// Both bodies are fixed for the life of the process, so each is hashed
	// once and compressed at most once per coding.
	playground := newStaticBody(
		fmt.Appendf(nil, apiPlaygroundHTML, html.EscapeString(basePath+"/")),
	)
	spec := newStaticBody(specJSON)
	source := newStaticBody(specYAML)

	serveYAML := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", openAPIYAMLMediaType)
		source.serve(w, r)
	}

	negotiated = func(w http.ResponseWriter, r *http.Request) {
		ct := ContentTypeFromContext(r.Context())
		w.Header().Set("Cache-Control", "public, max-age=3600")
		switch ct {
		case ContentTypeHTML:
			w.Header().Set("Content-Type", "text/html")
			w.Header().
				Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'")
			playground.serve(w, r)
		case ContentTypeJSON:
			// The default for content negotiation, including */*.
			w.Header().Set("Content-Type", "application/json")
			spec.serve(w, r)
		case ContentTypeYAML:
			serveYAML(w, r)
		default:
			notAcceptable(
				w, r,
				"application/json, text/html, "+openAPIYAMLMediaType,
			)
		}
	}

	yamlOnly = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		serveYAML(w, r)
	}

	return negotiated, yamlOnly
}

// HandleScalarJS serves the embedded Scalar API reference JavaScript bundle.
func HandleScalarJS(js []byte) http.HandlerFunc {
	bundle := newStaticBody(js)

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		bundle.serve(w, r)
	}
}

// rebaseServers prefixes each root-relative server URL with the base path,
// which the authored document cannot know.
func rebaseServers(specYAML []byte, basePath string) ([]byte, error) {
	if basePath == "" {
		return specYAML, nil
	}

	var document yaml.Node
	if err := yaml.Unmarshal(specYAML, &document); err != nil {
		return nil, fmt.Errorf("is not valid YAML: %w", err)
	}

	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("does not parse to an object")
	}

	root := document.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "servers" {
			continue
		}

		for _, server := range root.Content[i+1].Content {
			for j := 0; j+1 < len(server.Content); j += 2 {
				url := server.Content[j+1]
				if server.Content[j].Value == "url" && strings.HasPrefix(url.Value, "/") {
					url.Value = basePath + url.Value
				}
			}
		}
	}

	// yaml.Marshal indents by four; the authored file uses two.
	var out bytes.Buffer

	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)

	if err := encoder.Encode(&document); err != nil {
		return nil, err
	}

	if err := encoder.Close(); err != nil {
		return nil, err
	}

	return out.Bytes(), nil
}

// yamlDocument parses a spec into the JSON-shaped object both descriptions
// are served from.
func yamlDocument(raw []byte) (map[string]any, error) {
	var parsed any
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("not valid YAML: %w", err)
	}

	doc, ok := convertYAMLToJSON(parsed).(map[string]any)
	if !ok {
		return nil, errors.New("does not parse to an object")
	}

	return doc, nil
}

// convertYAMLToJSON recursively converts yaml.v3 map[string]any types to
// JSON-compatible types. Does not handle map[any]any (non-string keys).
func convertYAMLToJSON(v any) any {
	switch v := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(v))
		for k, val := range v {
			m[k] = convertYAMLToJSON(val)
		}
		return m
	case []any:
		for i, val := range v {
			v[i] = convertYAMLToJSON(val)
		}
		return v
	default:
		return v
	}
}
