//go:build e2e

package e2e_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// This file is the OAuth lane's self-enforcing gate, the counterpart to
// TestEveryReadRouteIsDrivenOrExcused and TestEveryMCPToolIsDrivenOrExcused.
//
// contract.Routes() cannot supply the inventory: internal/api/router.go
// registers the authorization server's endpoints indirectly, through
// `cfg.OAuthRoutes(mux, "")`, so the patterns never appear as literals in the
// file that inventory is parsed from. They appear in
// internal/mcp/oauth.Server.RegisterRoutes instead, which is what this file
// parses. A new OAuth endpoint therefore fails TestEveryOAuthEndpointIsDriven
// rather than going untested.

// oauthRoutesSource is the file the OAuth endpoint inventory is parsed from,
// relative to this package's directory.
const oauthRoutesSource = "../../internal/mcp/oauth/server.go"

// oauthEndpoints returns every pattern Server.RegisterRoutes attaches to the
// mux, as "METHOD /path".
//
// The patterns are written as `"GET " + basePath + "/oauth/authorize"`. The
// basePath identifier contributes nothing here because internal/api/router.go
// registers the routes with an empty base path (`cfg.OAuthRoutes(mux, "")`),
// which is also what this lane's SUT runs with — CETACEAN_BASE_PATH is unset.
// Only an identifier literally named basePath is treated that way, so a
// pattern built from some other variable fails to parse rather than silently
// yielding a shortened path.
func oauthEndpoints(t *testing.T) []string {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, oauthRoutesSource, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", oauthRoutesSource, err)
	}

	var (
		found  []string
		seenFn bool
	)

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "RegisterRoutes" || fn.Recv == nil {
			continue
		}

		seenFn = true

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "HandleFunc" || len(call.Args) == 0 {
				return true
			}

			pattern, ok := oauthPatternLiteral(call.Args[0])
			if !ok {
				t.Errorf(
					"%s: HandleFunc pattern at line %d is not a literal this "+
						"inventory can read; teach oauthPatternLiteral about it",
					oauthRoutesSource, fset.Position(call.Pos()).Line,
				)

				return true
			}

			found = append(found, pattern)

			return true
		})
	}

	if !seenFn {
		t.Fatalf(
			"%s declares no method RegisterRoutes; the OAuth endpoint inventory "+
				"is reading the wrong file or the method was renamed",
			oauthRoutesSource,
		)
	}

	sort.Strings(found)

	return slices.Compact(found)
}

// oauthPatternLiteral folds a `"GET " + basePath + "/oauth/authorize"`
// expression down to the string it evaluates to with an empty base path.
func oauthPatternLiteral(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}

		value, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}

		return value, true

	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}

		left, okLeft := oauthPatternLiteral(e.X)
		right, okRight := oauthPatternLiteral(e.Y)

		if !okLeft || !okRight {
			return "", false
		}

		return left + right, true

	case *ast.Ident:
		// The only non-literal the patterns are allowed to name. Anything
		// else is reported, because it would change the path this lane
		// drives without changing the inventory.
		if e.Name != "basePath" {
			return "", false
		}

		return "", true

	default:
		return "", false
	}
}

// drivenOAuthEndpoints names, for each endpoint the inventory reports, the
// case in this lane that drives it. The value is prose a reader can check
// against the test it names — not a function, because several endpoints are
// driven many times over by the flow helpers rather than once by a single
// case.
var drivenOAuthEndpoints = map[string]string{
	"GET /.well-known/oauth-authorization-server": "TestMCPOAuthFlow/discovery — fetched by " +
		"discoverOAuth, which follows the same chain a real client does: 401 → " +
		"WWW-Authenticate → PRM → authorization_servers → AS metadata.",

	"GET /.well-known/openid-configuration": "TestMCPOAuthFlow/discovery — asserted to serve " +
		"the identical document as the RFC 8414 path it aliases.",

	"GET /.well-known/oauth-protected-resource": "TestMCPOAuthFlow/discovery — the first " +
		"document discoverOAuth fetches, named by the 401's resource_metadata.",

	"POST /oauth/register": "TestMCPOAuthFlow/register and its four rejection cases; " +
		"TestMCPOAuthDCRRateLimit drives the per-IP limit; TestMCPOAuthWithoutDCROrCIMD " +
		"asserts the endpoint is absent when DCR is off.",

	"GET /oauth/authorize": "TestMCPOAuthFlow/consent_page and every flow that reaches a " +
		"code, plus the unauthenticated, unregistered-redirect, non-S256 and " +
		"missing-resource refusals.",

	"POST /oauth/authorize": "TestMCPOAuthFlow/approve, /deny and the two CSRF cases.",

	"POST /oauth/token": "TestMCPOAuthFlow/token_exchange, /pkce_mismatch, /code_is_single_use, " +
		"/refresh_rotates and TestMCPOAuthStateSurvivesARestart.",

	"POST /oauth/revoke": "TestMCPOAuthFlow/revocation_ends_the_grant.",
}

// excusedOAuthEndpoints carries a reason for any endpoint this lane does not
// drive. It is empty: all eight are driven. The map is kept so a new endpoint
// has somewhere to go other than silence — an excuse is a claim a reviewer can
// weigh, an omission is not.
var excusedOAuthEndpoints = map[string]string{}

// TestEveryOAuthEndpointIsDriven requires every pattern
// oauth.Server.RegisterRoutes attaches to appear in drivenOAuthEndpoints or
// excusedOAuthEndpoints, and requires both maps to name endpoints that still
// exist.
func TestEveryOAuthEndpointIsDriven(t *testing.T) {
	endpoints := oauthEndpoints(t)

	if len(endpoints) == 0 {
		t.Fatal("the OAuth endpoint inventory is empty; the parser found nothing to gate on")
	}

	live := make(map[string]bool, len(endpoints))

	for _, endpoint := range endpoints {
		live[endpoint] = true

		if _, ok := drivenOAuthEndpoints[endpoint]; ok {
			continue
		}

		reason, ok := excusedOAuthEndpoints[endpoint]
		if !ok {
			t.Errorf(
				"%s is neither driven nor excused; add coverage and name it in "+
					"drivenOAuthEndpoints, or a reason in excusedOAuthEndpoints",
				endpoint,
			)

			continue
		}

		if strings.TrimSpace(reason) == "" {
			t.Errorf("%s has an empty excuse reason", endpoint)
		}
	}

	for endpoint := range drivenOAuthEndpoints {
		if !live[endpoint] {
			t.Errorf(
				"drivenOAuthEndpoints has a stale entry %q: RegisterRoutes no longer "+
					"attaches it",
				endpoint,
			)
		}
	}

	for endpoint := range excusedOAuthEndpoints {
		if !live[endpoint] {
			t.Errorf(
				"excusedOAuthEndpoints has a stale entry %q: RegisterRoutes no longer "+
					"attaches it",
				endpoint,
			)
		}
	}

	t.Logf(
		"OAuth endpoints: %d driven, %d excused",
		len(drivenOAuthEndpoints), len(excusedOAuthEndpoints),
	)
}
