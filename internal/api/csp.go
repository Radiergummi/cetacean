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

// InlineScriptHashes returns a CSP source token per inline <script> in the
// SPA's index.html, as `'sha256-<base64>'`.
//
// The theme has to be applied before the first paint or a dark-mode reader sees
// a white flash on every load, which means an inline script in <head>; the
// policy is `default-src 'self'` with no `script-src`, so that script needs a
// hash to run at all. Hashing the document the server is about to serve — at
// startup, rather than pasting a digest into the policy — is what keeps the two
// from drifting: an edit to index.html changes the hash the header carries,
// with nothing to remember to update.
//
// Scripts carrying `src` are skipped: they load from 'self' already.
func InlineScriptHashes(fsys fs.FS) ([]string, error) {
	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return nil, fmt.Errorf("csp: reading index.html: %w", err)
	}

	var hashes []string
	for _, match := range scriptTag.FindAllSubmatch(index, -1) {
		attrs, body := string(match[1]), match[2]
		if strings.Contains(strings.ToLower(attrs), "src=") || len(body) == 0 {
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
