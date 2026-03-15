package auth

import (
	"fmt"
	"math/big"
	"net/http"
)

// CertProvider authenticates requests using mTLS client certificates.
// It supports SPIFFE URI SANs for workload identity.
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

	// Identity extraction priority per design spec:
	// 1. SPIFFE URI SAN → subject
	// 2. Email SAN → subject (if CN is empty)
	// 3. CN → subject + display name
	for _, uri := range cert.URIs {
		if uri.Scheme == "spiffe" {
			id.Subject = uri.String()
			id.Raw["spiffe_id"] = uri.String()
			if id.DisplayName == "" {
				id.DisplayName = uri.Path
			}
			break
		}
	}

	if id.Subject == "" && id.Email != "" {
		id.Subject = id.Email
	}

	return id, nil
}

func (p *CertProvider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/whoami", WhoamiHandler(p))
}

func formatSerial(n *big.Int) string {
	if n == nil {
		return ""
	}
	return fmt.Sprintf("%x", n)
}
