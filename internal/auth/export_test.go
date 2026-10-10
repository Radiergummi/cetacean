package auth

// NewTailscaleProviderWithClient builds a provider over a stub WhoIs client.
func NewTailscaleProviderWithClient(client WhoIsClient) *TailscaleProvider {
	return &TailscaleProvider{client: client}
}
