package api

import (
	"bytes"
	"errors"
	"maps"
	"net/http"
	"slices"
	"sync/atomic"

	json "github.com/goccy/go-json"
	"gopkg.in/yaml.v3"
)

const (
	asyncAPIPath     = "/api/asyncapi"
	asyncAPIYAMLPath = asyncAPIPath + ".yaml"

	// asyncAPIMediaTypeBase is the type without its version parameter, for
	// the places that compare types rather than state them.
	asyncAPIMediaTypeBase = "application/vnd.aai.asyncapi+json"
	asyncAPIMediaType     = asyncAPIMediaTypeBase + ";version=3.0.0"

	asyncAPIYAMLMediaType = "application/vnd.aai.asyncapi+yaml;version=3.0.0"
)

// asyncAPIRendering is one representation of the document as one origin sees
// it.
type asyncAPIRendering struct {
	key  string
	body *staticBody
}

// HandleAsyncAPI returns the negotiated handler and the one behind the .yaml
// address. Anything that is not a YAML request gets JSON, text/html included:
// there is no SPA route here.
//
// AsyncAPI 3.0 requires host on a server object, so the body varies by scheme,
// host and base path and is retained under that key. One slot per
// representation, not a map: origin falls back to r.Host, so the key is
// caller-controlled and a map would grow without bound. A miss is what every
// request with an unseen Host costs, so it renders only the representation
// being served rather than both.
func HandleAsyncAPI(specYAML []byte) (negotiated, yamlOnly http.HandlerFunc) {
	doc, err := yamlDocument(specYAML)
	if err != nil {
		panic("asyncapi spec " + err.Error())
	}

	source, err := newAsyncAPISource(specYAML)
	if err != nil {
		panic("asyncapi spec " + err.Error())
	}

	var currentJSON, currentYAML atomic.Pointer[asyncAPIRendering]

	render := func(r *http.Request, asYAML bool) (*staticBody, error) {
		slot := &currentJSON
		if asYAML {
			slot = &currentYAML
		}

		scheme, host := origin(r)
		base := BasePathFromContext(r.Context())
		key := scheme + "\x00" + host + "\x00" + base

		if rendering := slot.Load(); rendering != nil && rendering.key == key {
			return rendering.body, nil
		}

		fields := asyncAPIServerFields(scheme, host, base)

		var (
			raw []byte
			err error
		)

		if asYAML {
			raw, err = source.render(fields)
		} else {
			raw, err = renderAsyncAPIJSON(doc, fields)
		}

		if err != nil {
			return nil, err
		}

		body := newStaticBody(raw)
		slot.Store(&asyncAPIRendering{key: key, body: body})

		return body, nil
	}

	serve := func(w http.ResponseWriter, r *http.Request, asYAML bool) {
		body, err := render(r, asYAML)
		if err != nil {
			writeErrorCode(w, r, "API009", "failed to serialize response")

			return
		}

		w.Header().Set("Cache-Control", "public, max-age=3600")

		if asYAML {
			w.Header().Set("Content-Type", asyncAPIYAMLMediaType)
		} else {
			w.Header().Set("Content-Type", asyncAPIMediaType)
		}

		body.serve(w, r)
	}

	negotiated = func(w http.ResponseWriter, r *http.Request) {
		serve(w, r, ContentTypeFromContext(r.Context()) == ContentTypeYAML)
	}

	yamlOnly = func(w http.ResponseWriter, r *http.Request) {
		serve(w, r, true)
	}

	return negotiated, yamlOnly
}

// asyncAPIServerFields is the server object both representations inject, named
// once so they cannot disagree.
func asyncAPIServerFields(scheme, host, base string) [][2]string {
	fields := [][2]string{
		{"host", host},
		{"protocol", scheme},
		{"description", "This deployment."},
	}

	if base != "" {
		fields = append(fields, [2]string{"pathname", base})
	}

	return fields
}

// renderAsyncAPIJSON marshals doc with the server block one origin gets.
func renderAsyncAPIJSON(doc map[string]any, fields [][2]string) ([]byte, error) {
	server := make(map[string]any, len(fields))
	for _, field := range fields {
		server[field[0]] = field[1]
	}

	// Shallow copy: nothing below servers is written.
	described := make(map[string]any, len(doc))
	maps.Copy(described, doc)

	described["servers"] = map[string]any{"self": server}

	return json.Marshal(described)
}

// asyncAPISource is the authored document as a node tree. The YAML is spliced
// rather than re-encoded from a map, which would lose the comments and key
// order a reader came for.
type asyncAPISource struct {
	root *yaml.Node

	// serversAt indexes the servers *value*, or -1 if there is none.
	serversAt int
}

func newAsyncAPISource(specYAML []byte) (*asyncAPISource, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(specYAML, &document); err != nil {
		return nil, errors.New("is not valid YAML: " + err.Error())
	}

	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("does not parse to an object")
	}

	source := &asyncAPISource{root: document.Content[0], serversAt: -1}

	for i := 0; i+1 < len(source.root.Content); i += 2 {
		if source.root.Content[i].Value == "servers" {
			source.serversAt = i + 1

			break
		}
	}

	return source, nil
}

// render writes the document for one origin. The root mapping is copied so
// concurrent renders never share the slice they write.
func (s *asyncAPISource) render(fields [][2]string) ([]byte, error) {
	servers := &yaml.Node{
		Kind:    yaml.MappingNode,
		Content: []*yaml.Node{yamlScalar("self"), yamlMapping(fields)},
	}

	root := *s.root
	root.Content = slices.Clone(s.root.Content)

	if s.serversAt >= 0 {
		root.Content[s.serversAt] = servers
	} else {
		root.Content = append(root.Content, yamlScalar("servers"), servers)
	}

	// yaml.Marshal indents by four; the authored file uses two.
	var out bytes.Buffer

	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)

	if err := encoder.Encode(&yaml.Node{
		Kind:    yaml.DocumentNode,
		Content: []*yaml.Node{&root},
	}); err != nil {
		return nil, err
	}

	if err := encoder.Close(); err != nil {
		return nil, err
	}

	return out.Bytes(), nil
}

func yamlMapping(fields [][2]string) *yaml.Node {
	node := &yaml.Node{
		Kind:    yaml.MappingNode,
		Content: make([]*yaml.Node, 0, len(fields)*2),
	}

	for _, field := range fields {
		node.Content = append(node.Content, yamlScalar(field[0]), yamlScalar(field[1]))
	}

	return node
}

func yamlScalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}
