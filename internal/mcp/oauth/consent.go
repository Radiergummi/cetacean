package oauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"html/template"
	"net/http"
	"time"
)

const csrfCookieName = "mcp_csrf_nonce"
const csrfCookieTTL = 10 * time.Minute

// consentTemplate is the minimal HTML consent page rendered for the user.
var consentTemplate = template.Must(template.New("consent").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Authorization Request</title>
<style>
  body { font-family: system-ui, sans-serif; max-width: 500px; margin: 80px auto; padding: 0 20px; color: #1a1a1a; }
  h1 { font-size: 1.4rem; margin-bottom: 0.5rem; }
  .client { background: #f4f4f5; border-radius: 8px; padding: 16px; margin: 20px 0; }
  .client-name { font-weight: 600; font-size: 1.1rem; }
  .badge { display: inline-block; font-size: 0.75rem; padding: 2px 8px; border-radius: 12px; margin-left: 8px; }
  .badge-verified { background: #dcfce7; color: #15803d; }
  .badge-self { background: #fef9c3; color: #854d0e; }
  .redirect { font-size: 0.85rem; color: #555; word-break: break-all; margin-top: 8px; }
  .identity { background: #eff6ff; border-radius: 8px; padding: 12px 16px; margin: 20px 0; font-size: 0.9rem; }
  .actions { display: flex; gap: 12px; margin-top: 24px; }
  button { flex: 1; padding: 10px; border: none; border-radius: 6px; font-size: 1rem; cursor: pointer; }
  .btn-approve { background: #2563eb; color: #fff; }
  .btn-approve:hover { background: #1d4ed8; }
  .btn-deny { background: #f1f5f9; color: #374151; }
  .btn-deny:hover { background: #e2e8f0; }
  .warning { background: #fff7ed; border-left: 4px solid #f97316; padding: 10px 14px; border-radius: 4px; font-size: 0.85rem; margin: 16px 0; }
</style>
</head>
<body>
<h1>Authorization Request</h1>
<div class="client">
  <div>
    <span class="client-name">{{.ClientName}}</span>
    {{if .Verified}}<span class="badge badge-verified">Verified</span>{{else}}<span class="badge badge-self">Self-registered</span>{{end}}
  </div>
  <div class="redirect">Redirect: {{.RedirectURI}}</div>
</div>
<div class="identity">
  Authorizing as: <strong>{{.Subject}}</strong>{{if .Email}} ({{.Email}}){{end}}
</div>
<div class="warning">
  This will grant <strong>{{.ClientName}}</strong> access to your Cetacean instance. Only approve if you trust this application.
</div>
{{if .Remembered}}<div class="warning">Approving will be remembered for <strong>{{.ClientName}}</strong> until you revoke its access.</div>{{end}}
<form method="POST" action="{{.ActionURL}}">
  <input type="hidden" name="response_type" value="{{.ResponseType}}">
  <input type="hidden" name="client_id" value="{{.ClientID}}">
  <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
  <input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
  <input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
  <input type="hidden" name="state" value="{{.State}}">
  <input type="hidden" name="resource" value="{{.Resource}}">
  <input type="hidden" name="consent_fingerprint" value="{{.Fingerprint}}">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <div class="actions">
    <button type="submit" name="decision" value="approve" class="btn-approve">Approve</button>
    <button type="submit" name="decision" value="deny" class="btn-deny">Deny</button>
  </div>
</form>
</body>
</html>
`))

// errorTemplate is the minimal HTML error page for non-redirectable errors.
var errorTemplate = template.Must(template.New("error").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>Authorization Error</title>
<style>
  body { font-family: system-ui, sans-serif; max-width: 500px; margin: 80px auto; padding: 0 20px; color: #1a1a1a; }
  h1 { font-size: 1.4rem; }
  .error { background: #fef2f2; border-left: 4px solid #ef4444; padding: 12px 16px; border-radius: 4px; }
</style>
</head>
<body>
<h1>Authorization Error</h1>
<div class="error">{{.Message}}</div>
</body>
</html>
`))

// consentData holds the template variables for the consent page.
type consentData struct {
	ClientName          string
	Verified            bool
	Remembered          bool
	RedirectURI         string
	Subject             string
	Email               string
	ActionURL           string
	ResponseType        string
	ClientID            string
	CodeChallenge       string
	CodeChallengeMethod string
	State               string
	Resource            string

	// Fingerprint hashes the metadata this page was rendered from. It rides
	// along in a hidden field and is covered by CSRFToken, so the POST can
	// tell whether the client changed while the user was deciding.
	Fingerprint string
	CSRFToken   string
}

// renderConsent writes the consent page with security headers.
func renderConsent(w http.ResponseWriter, data consentData) {
	setConsentHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = consentTemplate.Execute(w, data)
}

// renderErrorPage writes a non-redirectable error page.
func renderErrorPage(w http.ResponseWriter, status int, message string) {
	setConsentHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = errorTemplate.Execute(w, map[string]string{"Message": message})
}

// setConsentHeaders sets security headers that prevent framing and caching.
// The consent page carries the CSRF token, OAuth state, code_challenge,
// redirect_uri and authenticated user identity in hidden form fields — any
// shared cache or browser back-button cache would replay that to a different
// user. Cache-Control: no-store matches the token-response handler.
func setConsentHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

// consentFingerprintField is the hidden form field carrying the fingerprint of
// the client metadata the consent page was rendered from. It is not a secret —
// the CSRF HMAC over it is what makes it unforgeable.
const consentFingerprintField = "consent_fingerprint"

// csrfMAC derives the CSRF token from the nonce and the request parameters it
// must stay bound to. Both issuing and verifying go through here so the two
// cannot drift apart.
//
// The fingerprint is covered because the recorded approval must be bound to
// what the user was *shown*: the metadata is resolved on GET to render the
// page and again on POST to act on it, and a CIMD document can change (or its
// cache entry lapse) in between. Folding it into the MAC that is already
// verified on every POST carries the GET's view forward without adding a
// second piece of trust machinery.
//
// Fields are length-prefixed via hashField rather than joined with a
// separator. state is client-chosen and may contain any byte, so a plain
// "nonce|state|fingerprint" would let one field's content spell another's and
// a token issued for one pair verify for a different one. This is the same
// discipline consentFingerprint and consentKey already apply, for the same
// reason.
func csrfMAC(signingKey []byte, nonce, state, fingerprint string) string {
	mac := hmac.New(sha256.New, signingKey)
	hashField(mac, nonce)
	hashField(mac, state)
	hashField(mac, fingerprint)

	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// issueCSRFNonce generates a random nonce, sets a short-lived signed cookie,
// and returns the CSRF token (an HMAC over nonce, state and the client
// metadata fingerprint the page is being rendered from). The cookie is
// HttpOnly and SameSite=Strict. When secure is true (issuer is HTTPS) the
// cookie is also marked Secure.
func issueCSRFNonce(
	w http.ResponseWriter,
	signingKey []byte,
	state string,
	fingerprint string,
	secure bool,
) (token string, nonce string) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("mcp/oauth: crypto/rand failure: " + err.Error())
	}
	nonce = base64.RawURLEncoding.EncodeToString(b)
	token = csrfMAC(signingKey, nonce, state, fingerprint)

	//nolint:gosec // G124: cookie is HttpOnly + SameSite=Strict; Secure is true on HTTPS issuers and intentionally off only for loopback HTTP dev, which gosec can't prove from the variable.
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    nonce,
		MaxAge:   int(csrfCookieTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
		Path:     "/",
	})

	return token, nonce
}

// clearCSRFCookie tells the browser to drop the nonce cookie issued at the
// start of the consent flow. Called whenever the flow terminates so a stale
// cookie cannot survive past its useful life.
func clearCSRFCookie(w http.ResponseWriter, secure bool) {
	//nolint:gosec // G124: cookie is HttpOnly + SameSite=Strict; Secure is true on HTTPS issuers and intentionally off only for loopback HTTP dev, which gosec can't prove from the variable.
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
	})
}

// verifyCSRFToken validates the CSRF token from the form against the nonce
// stored in the cookie. A token verifies only for the state and the metadata
// fingerprint it was issued for, so the caller can trust the submitted
// fingerprint as the one the consent page actually displayed.
func verifyCSRFToken(r *http.Request, signingKey []byte) bool {
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil {
		return false
	}

	expected := csrfMAC(
		signingKey,
		cookie.Value,
		r.FormValue("state"),
		r.FormValue(consentFingerprintField),
	)

	return hmac.Equal([]byte(r.FormValue("csrf_token")), []byte(expected))
}
