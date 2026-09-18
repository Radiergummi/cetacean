// Package spec stands in for internal/spec so analysistest can resolve the
// import path the analyzer matches on.
package spec

import "testing"

func Satisfies(t testing.TB, ids ...string) {}
