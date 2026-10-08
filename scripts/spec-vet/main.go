// Command spec-vet checks how tests claim requirements. It runs as a vet tool
// — `go vet -vettool=spec-vet ./...` — so it sees the types the parser-based
// scan in scripts/spec-gate cannot, and reports at the call site.
package main

import "golang.org/x/tools/go/analysis/unitchecker"

func main() { unitchecker.Main(Analyzer) }
