package auth

import (
	"crypto/x509"
	"encoding/base64"
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
	cert, err := clientCertificate(r)
	if err != nil {
		return nil, err
	}

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

func (p *CertProvider) RegisterRoutes(_ *http.ServeMux) {}

// clientCertificate returns the certificate identifying the client: the one
// presented on this connection, or — when a trusted proxy terminated TLS
// instead — the one it forwarded in the RFC 9440 Client-Cert header.
//
// A certificate presented here was verified here, so it always wins. A
// forwarded one is believed only because the proxy that verified it is
// trusted, which is why the verdict recorded at the edge gates it: RFC 9440 §3
// requires that an origin server "MUST only accept the Client-Cert and
// Client-Cert-Chain header fields from a trusted TTRP". Anyone can set a
// header.
//
// Client-Cert-Chain is deliberately not read: it carries the issuer chain for
// a party doing its own validation, which the proxy has already done.
func clientCertificate(r *http.Request) (*x509.Certificate, error) {
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		return r.TLS.PeerCertificates[0], nil
	}

	headers := r.Header.Values("Client-Cert")
	if len(headers) == 0 || !FromTrustedProxy(r.Context()) {
		return nil, &AuthError{
			Msg:             "client certificate required",
			WWWAuthenticate: "mutual-tls",
		}
	}

	// A TTRP replaces the field rather than appending to it, so a second value
	// means one of the two came from the client — and picking either would be
	// a guess at which.
	if len(headers) > 1 {
		return nil, &AuthError{
			Msg:             "Client-Cert appears more than once; the proxy must replace any header its client sent",
			WWWAuthenticate: "mutual-tls",
		}
	}

	der, err := decodeClientCert(headers[0])
	if err != nil {
		return nil, &AuthError{Msg: err.Error(), WWWAuthenticate: "mutual-tls"}
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, &AuthError{
			Msg:             fmt.Sprintf("Client-Cert does not carry a certificate: %v", err),
			WWWAuthenticate: "mutual-tls",
		}
	}

	return cert, nil
}

// decodeClientCert decodes an RFC 9440 Client-Cert value. It is an RFC 8941
// Byte Sequence: the DER certificate in base64 with no line breaks, delimited
// by a colon at each end. An empty one ("::") is well-formed and carries no
// certificate, which x509.ParseCertificate reports better than this can.
func decodeClientCert(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[0] != ':' || value[len(value)-1] != ':' {
		return nil, fmt.Errorf(
			"Client-Cert is not a byte sequence; expected :base64:, got %q",
			truncate(value),
		)
	}

	der, err := base64.StdEncoding.DecodeString(value[1 : len(value)-1])
	if err != nil {
		return nil, fmt.Errorf("Client-Cert is not valid base64: %w", err)
	}

	return der, nil
}

// truncate shortens a header value for an error message. A certificate is
// kilobytes of base64 and none of it helps a reader.
func truncate(s string) string {
	const limit = 32
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
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
			return "", fmt.Errorf(
				"certificate contains multiple SPIFFE URI SANs; X.509-SVID requires exactly one",
			)
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
