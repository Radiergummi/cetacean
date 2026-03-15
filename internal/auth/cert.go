package auth

import (
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
)

// CertProvider authenticates requests using mTLS client certificates.
// It supports SPIFFE URI SANs for workload identity per the X.509-SVID spec.
type CertProvider struct{}

func (p *CertProvider) Authenticate(_ http.ResponseWriter, r *http.Request) (*Identity, error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return nil, &AuthError{
			Msg:             "client certificate required",
			WWWAuthenticate: "mutual-tls",
		}
	}

	cert := r.TLS.PeerCertificates[0]

	id := &Identity{
		Subject:     cert.Subject.CommonName,
		DisplayName: cert.Subject.CommonName,
		Provider:    "cert",
		Groups:      cert.Subject.OrganizationalUnit,
		Raw: map[string]any{
			"serial":    formatSerial(cert.SerialNumber),
			"issuer_cn": cert.Issuer.CommonName,
			"not_after": cert.NotAfter.UTC().Format("2006-01-02T15:04:05Z"),
		},
	}

	if len(cert.EmailAddresses) > 0 {
		id.Email = cert.EmailAddresses[0]
	}

	// Extract SPIFFE ID from URI SANs per X.509-SVID spec.
	spiffeID, err := extractSPIFFEID(cert.URIs)
	if err != nil {
		return nil, &AuthError{
			Msg:             err.Error(),
			WWWAuthenticate: "mutual-tls",
		}
	}
	if spiffeID != "" {
		id.Subject = spiffeID
		id.Raw["spiffe_id"] = spiffeID
		if id.DisplayName == "" {
			// Use the path portion as display name for SPIFFE workloads.
			if u, err := url.Parse(spiffeID); err == nil {
				id.DisplayName = u.Path
			}
		}
	}

	// Fallback: email as subject when CN is empty.
	if id.Subject == "" && id.Email != "" {
		id.Subject = id.Email
	}

	if id.Subject == "" {
		return nil, &AuthError{
			Msg:             "certificate has no identifiable subject (no CN, email, or SPIFFE URI SAN)",
			WWWAuthenticate: "mutual-tls",
		}
	}

	return id, nil
}

func (p *CertProvider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/whoami", WhoamiHandler(p))
}

// extractSPIFFEID returns the SPIFFE ID from the URI SANs, or "" if none
// present. Returns an error if the cert contains multiple SPIFFE URIs
// (per X.509-SVID spec: exactly one required) or a malformed SPIFFE ID.
func extractSPIFFEID(uris []*url.URL) (string, error) {
	var spiffeURI *url.URL
	for _, uri := range uris {
		if uri.Scheme != "spiffe" {
			continue
		}
		if spiffeURI != nil {
			return "", fmt.Errorf("certificate contains multiple SPIFFE URI SANs; X.509-SVID requires exactly one")
		}
		spiffeURI = uri
	}
	if spiffeURI == nil {
		return "", nil
	}
	if err := validateSPIFFEID(spiffeURI); err != nil {
		return "", err
	}
	return spiffeURI.String(), nil
}

// validateSPIFFEID checks a parsed SPIFFE URI against the SPIFFE ID spec:
// https://github.com/spiffe/spiffe/blob/main/standards/SPIFFE.md
func validateSPIFFEID(u *url.URL) error {
	raw := u.String()
	if len(raw) > 2048 {
		return fmt.Errorf("SPIFFE ID exceeds 2048 bytes: %d", len(raw))
	}

	// Trust domain is the host component; must be non-empty.
	td := u.Host
	if td == "" {
		return fmt.Errorf("SPIFFE ID has empty trust domain: %s", raw)
	}
	if len(td) > 255 {
		return fmt.Errorf("SPIFFE ID trust domain exceeds 255 characters: %s", td)
	}
	for _, c := range td {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '.' && c != '-' && c != '_' {
			return fmt.Errorf("SPIFFE ID trust domain contains invalid character %q: %s", c, raw)
		}
	}

	// Path must start with / (if present).
	path := u.Path
	if path != "" && !strings.HasPrefix(path, "/") {
		return fmt.Errorf("SPIFFE ID path must start with /: %s", raw)
	}

	// Validate path segments: no empty segments, no "." or "..".
	if path != "" {
		for seg := range strings.SplitSeq(path[1:], "/") { // skip leading /
			if seg == "" {
				return fmt.Errorf("SPIFFE ID path contains empty segment: %s", raw)
			}
			if seg == "." || seg == ".." {
				return fmt.Errorf("SPIFFE ID path contains dot segment %q: %s", seg, raw)
			}
		}
	}

	// No query or fragment allowed.
	if u.RawQuery != "" {
		return fmt.Errorf("SPIFFE ID must not contain query: %s", raw)
	}
	if u.Fragment != "" {
		return fmt.Errorf("SPIFFE ID must not contain fragment: %s", raw)
	}

	return nil
}

func formatSerial(n *big.Int) string {
	if n == nil {
		return ""
	}
	return fmt.Sprintf("%x", n)
}
