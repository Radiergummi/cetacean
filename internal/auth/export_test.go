package auth

// NewTailscaleProviderWithClient builds a provider over a stub WhoIs client.
func NewTailscaleProviderWithClient(client WhoIsClient) *TailscaleProvider {
	return &TailscaleProvider{client: client}
}

// ClaimsToIdentity builds the identity an OIDC provider would from claims.
func ClaimsToIdentity(claims map[string]any) *Identity {
	return claimsToIdentity(claims)
}
