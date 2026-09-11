package contract

import (
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
)

// specSource is the OpenAPI document, relative to this package's directory.
const specSource = "../../api/openapi.yaml"

// Operation is one documented method-and-path pair.
type Operation struct {
	Method string
	Path   string
}

func (o Operation) String() string {
	return o.Method + " " + o.Path
}

var (
	specOnce sync.Once
	specOps  []Operation
	specErr  error
)

// SpecOperations returns every operation documented in api/openapi.yaml,
// sorted. Loading and validating the document costs tens of milliseconds, so it
// happens once per test binary.
func SpecOperations() ([]Operation, error) {
	specOnce.Do(func() { specOps, specErr = parseSpec(specSource) })

	return specOps, specErr
}

func parseSpec(path string) ([]Operation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	loader := openapi3.NewLoader()

	doc, err := loader.LoadFromData(data)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}

	if err := doc.Validate(loader.Context); err != nil {
		return nil, fmt.Errorf("validate %s: %w", path, err)
	}

	methods := []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions,
	}

	var out []Operation

	for path, item := range doc.Paths.Map() {
		for _, method := range methods {
			if item.GetOperation(method) != nil {
				out = append(out, Operation{Method: method, Path: path})
			}
		}
	}

	slices.SortFunc(out, func(a, b Operation) int {
		return strings.Compare(a.String(), b.String())
	})

	return out, nil
}
