package api

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// asyncAPISchema is the official AsyncAPI 3.0.0 meta-schema. It is embedded
// from a _test.go file so it is never linked into the binary.
//
//go:embed testdata/asyncapi-3.0.0.json
var asyncAPISchema []byte

const asyncAPISpecPath = "../../api/asyncapi.yaml"

// loadAsyncAPIDoc parses api/asyncapi.yaml into the JSON-shaped value every
// test here reads. The served document is the same value, so a test that
// passes against the file passes against the response.
func loadAsyncAPIDoc(t *testing.T) map[string]any {
	t.Helper()

	return loadYAMLDocument(t, asyncAPISpecPath)
}

// TestAsyncAPIDocumentIsValid validates api/asyncapi.yaml against the official
// AsyncAPI 3.0.0 meta-schema.
//
// The meta-schema's $refs are absolute http://asyncapi.com/ URLs, but every
// one of them resolves inside the file: each entry under definitions carries
// the matching $id. No loader is installed, so a ref that did not resolve
// locally would fail the compile rather than reach the network.
func TestAsyncAPIDocumentIsValid(t *testing.T) {
	const metaURL = "http://asyncapi.com/definitions/3.0.0/asyncapi.json"

	meta, err := jsonschema.UnmarshalJSON(bytes.NewReader(asyncAPISchema))
	if err != nil {
		t.Fatalf("the vendored meta-schema is not JSON: %v", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(metaURL, meta); err != nil {
		t.Fatalf("add meta-schema: %v", err)
	}

	schema, err := compiler.Compile(metaURL)
	if err != nil {
		t.Fatalf("compile meta-schema: %v", err)
	}

	encoded, err := json.Marshal(loadAsyncAPIDoc(t))
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}

	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("re-parse document: %v", err)
	}

	if err := schema.Validate(instance); err != nil {
		t.Fatalf("api/asyncapi.yaml is not valid AsyncAPI 3.0.0:\n%v", err)
	}
}

// TestAsyncAPISchemasMatchOpenAPI pins the two schemas asyncapi.yaml copies
// from openapi.yaml. They are copied rather than $ref'd because the correct
// reference differs between the file in the repository and the document as
// served, and one reference with two correct spellings is the trap
// /.well-known/jwks.json fell into from the other direction.
func TestAsyncAPISchemasMatchOpenAPI(t *testing.T) {
	openAPI := loadYAMLDocument(t, "../../api/openapi.yaml")
	asyncAPI := loadAsyncAPIDoc(t)

	for _, name := range []string{"SSEEvent", "LogLine"} {
		t.Run(name, func(t *testing.T) {
			want := schemaNamed(t, openAPI, name)
			got := schemaNamed(t, asyncAPI, name)

			if !reflect.DeepEqual(got, want) {
				t.Errorf(
					"asyncapi.yaml's %s has drifted from openapi.yaml's\n got: %#v\nwant: %#v",
					name, got, want,
				)
			}
		})
	}
}

// loadYAMLDocument parses any of the two specs into the same JSON shape.
func loadYAMLDocument(t *testing.T, path string) map[string]any {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var parsed any
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("%s is not valid YAML: %v", path, err)
	}

	doc, ok := convertYAMLToJSON(parsed).(map[string]any)
	if !ok {
		t.Fatalf("%s does not parse to an object", path)
	}

	return doc
}

// schemaNamed digs components.schemas.<name> out of either document.
func schemaNamed(t *testing.T, doc map[string]any, name string) any {
	t.Helper()

	components, ok := doc["components"].(map[string]any)
	if !ok {
		t.Fatal("document has no components")
	}

	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatal("document has no components.schemas")
	}

	schema, ok := schemas[name]
	if !ok {
		t.Fatalf("document has no components.schemas.%s", name)
	}

	return schema
}
