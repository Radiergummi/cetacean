package api

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
)

// scriptTag matches a <script> element and captures its attributes and body.
var scriptTag = regexp.MustCompile(`(?is)<script([^>]*)>(.*?)</script>`)

// srcAttr matches a `src` attribute on its own, so that `data-src` is not one.
var srcAttr = regexp.MustCompile(`(?i)(^|[\s/])src\s*=`)

// InlineScriptHashes returns a CSP source token per inline <script> in the SPA's
// index.html. The theme must be applied before first paint, which means an
// inline script, and `default-src 'self'` needs a hash for it to run. Hashing
// the document the server is about to serve keeps the two in step.
func InlineScriptHashes(fsys fs.FS) ([]string, error) {
	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return nil, fmt.Errorf("csp: reading index.html: %w", err)
	}

	var hashes []string
	for _, match := range scriptTag.FindAllSubmatch(index, -1) {
		attrs, body := match[1], match[2]
		if srcAttr.Match(attrs) || len(body) == 0 {
			continue
		}

		sum := sha256.Sum256(body)
		hashes = append(hashes, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}

	return hashes, nil
}

// contentSecurityPolicy builds the policy served on every response, allowing
// the inline scripts named by hash and nothing else.
func contentSecurityPolicy(inlineScriptHashes []string) string {
	scriptSrc := "'self'"
	if len(inlineScriptHashes) > 0 {
		scriptSrc += " " + strings.Join(inlineScriptHashes, " ")
	}

	return "default-src 'self'; script-src " + scriptSrc +
		"; style-src 'self' 'unsafe-inline'; img-src 'self' data:"
}
