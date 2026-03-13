package auth

import (
	"errors"
	"fmt"
	"math/big"
	"net/http"

	json "github.com/goccy/go-json"
)

// CertProvider authenticates requests using mTLS client certificates.
// It supports SPIFFE URI SANs for workload identity.
type CertProvider struct{}

func (p *CertProvider) Authenticate(_ http.ResponseWriter, r *http.Request) (*Identity, error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return nil, errors.New("client certificate required")
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

	// Check for SPIFFE URI SAN.
	for _, uri := range cert.URIs {
		if uri.Scheme == "spiffe" {
			id.Subject = uri.String()
			id.Raw["spiffe_id"] = uri.String()
			break
		}
	}

	return id, nil
}

func (p *CertProvider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/whoami", func(w http.ResponseWriter, r *http.Request) {
		id, err := p.Authenticate(nil, r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(id)
	})
}

func formatSerial(n *big.Int) string {
	if n == nil {
		return ""
	}
	return fmt.Sprintf("%x", n)
}
