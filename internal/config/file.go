package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// fileConfig mirrors the TOML schema. Pointer fields distinguish
// "not set" (nil) from "set to zero value".
type fileConfig struct {
	Server  *fileServer  `toml:"server"`
	Docker  *fileDocker  `toml:"docker"`
	Prom    *fileProm    `toml:"prometheus"`
	Logging *fileLogging `toml:"logging"`
	Storage *fileStorage `toml:"storage"`
	TLS     *fileTLS     `toml:"tls"`
	Auth    *fileAuth    `toml:"auth"`
}

type fileServer struct {
	ListenAddr      *string  `toml:"listen_addr"`
	Pprof           *bool    `toml:"pprof"`
	SSE             *fileSSE `toml:"sse"`
	OperationsLevel *int     `toml:"operations_level"`
}

type fileSSE struct {
	BatchInterval *string `toml:"batch_interval"`
}

type fileDocker struct {
	Host *string `toml:"host"`
}

type fileProm struct {
	URL *string `toml:"url"`
}

type fileLogging struct {
	Level  *string `toml:"level"`
	Format *string `toml:"format"`
}

type fileStorage struct {
	DataDir  *string `toml:"data_dir"`
	Snapshot *bool   `toml:"snapshot"`
}

type fileTLS struct {
	Cert *string `toml:"cert"`
	Key  *string `toml:"key"`
}

type fileAuth struct {
	Mode      *string          `toml:"mode"`
	OIDC      *fileAuthOIDC    `toml:"oidc"`
	Tailscale *fileAuthTS      `toml:"tailscale"`
	Cert      *fileAuthCert    `toml:"cert"`
	Headers   *fileAuthHeaders `toml:"headers"`
}

type fileAuthOIDC struct {
	Issuer       *string `toml:"issuer"`
	ClientID     *string `toml:"client_id"`
	ClientSecret *string `toml:"client_secret"`
	RedirectURL  *string `toml:"redirect_url"`
	Scopes       *string `toml:"scopes"`
	SessionKey   *string `toml:"session_key"`
}

type fileAuthTS struct {
	Mode       *string `toml:"mode"`
	AuthKey    *string `toml:"authkey"`
	Hostname   *string `toml:"hostname"`
	StateDir   *string `toml:"state_dir"`
	Capability *string `toml:"capability"`
}

type fileAuthCert struct {
	CA *string `toml:"ca"`
}

type fileAuthHeaders struct {
	Subject        *string `toml:"subject"`
	Name           *string `toml:"name"`
	Email          *string `toml:"email"`
	Groups         *string `toml:"groups"`
	SecretHeader   *string `toml:"secret_header"`
	SecretValue    *string `toml:"secret_value"`
	TrustedProxies *string `toml:"trusted_proxies"`
}

// LoadFile reads and parses the TOML config file at path.
// Returns nil config (not an error) if path is empty.
func LoadFile(path string) (*fileConfig, error) {
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var fc fileConfig
	if err := toml.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}
	return &fc, nil
}
