// Command dump-errors writes the well-known error catalog to stdout as JSON.
//
// The website renders its error reference from this rather than from a copy an
// author maintains, so the published page cannot drift from the registry the
// server answers with. It is run by website/package.json's sync-assets step
// before every dev server and every build.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/radiergummi/cetacean/internal/api"
)

type catalog struct {
	Domains []api.ErrorDomain `json:"domains"`
	Errors  []api.ErrorDef    `json:"errors"`
}

func encode() ([]byte, error) {
	return json.MarshalIndent(catalog{
		Domains: api.ErrorDomains,
		Errors:  api.ErrorDefs(),
	}, "", "  ")
}

func main() {
	data, err := encode()
	if err != nil {
		fmt.Fprintln(os.Stderr, "dump-errors:", err)
		os.Exit(1)
	}

	if _, err := os.Stdout.Write(append(data, '\n')); err != nil {
		fmt.Fprintln(os.Stderr, "dump-errors:", err)
		os.Exit(1)
	}
}
