// Command dump-errors writes the well-known error catalog to stdout as JSON.
// The website renders its error reference from this rather than a hand-kept
// copy, so the page cannot drift from the registry the server answers with.
// website/package.json's sync-assets step runs it before every build.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/radiergummi/cetacean/internal/api/problem"
)

type catalog struct {
	Domains []problem.Domain `json:"domains"`
	Errors  []problem.Def    `json:"errors"`
}

func encode() ([]byte, error) {
	return json.MarshalIndent(catalog{
		Domains: problem.Domains,
		Errors:  problem.Defs(),
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
