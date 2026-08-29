package sbom

import _ "embed"

// notices.txt is a byte-identical copy of THIRD_PARTY_LICENSES at the repo
// root, written by 'make sbom'. The duplicate exists because go:embed cannot
// reference a parent directory.
//
//go:embed notices.txt
var notices string

// Notices returns the full third-party attribution document: the curated
// preamble followed by every bundled dependency's license and notice text.
// Served at GET /-/notices.
func Notices() string { return notices }
