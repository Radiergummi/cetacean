package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const configFileName = "cetacean.toml"

// DiscoverConfigFile searches standard locations for a config file and
// returns the path to the first one found. Returns "" if none exists.
//
// Search order:
//  1. ./cetacean.toml (working directory)
//  2. $XDG_CONFIG_HOME/cetacean/cetacean.toml (or ~/.config/cetacean/cetacean.toml)
//  3. $HOME/.cetacean.toml
//  4. /etc/cetacean/cetacean.toml
func DiscoverConfigFile() string {
	candidates := []string{
		configFileName,
	}

	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "cetacean", configFileName))
	} else if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "cetacean", configFileName))
	}

	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".cetacean.toml"))
	}

	candidates = append(candidates, filepath.Join("/etc", "cetacean", configFileName))

	for _, path := range candidates {
		//nolint:gosec // paths are from well-known config locations, not user input
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

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
	Sizing  *fileSizing  `toml:"sizing"`
	ACL     *fileACL     `toml:"acl"`
}

type fileSizing struct {
	Headroom   *float64              `toml:"headroom_multiplier"`
	Thresholds *fileSizingThresholds `toml:"thresholds"`
}

type fileSizingThresholds struct {
	OverProvisioned  *float64 `toml:"over_provisioned"`
	ApproachingLimit *float64 `toml:"approaching_limit"`
	AtLimit          *float64 `toml:"at_limit"`
	Lookback         *string  `toml:"lookback"`
}

type fileServer struct {
	ListenAddr      *string   `toml:"listen_addr"`
	Pprof           *bool     `toml:"pprof"`
	SelfMetrics     *bool     `toml:"self_metrics"`
	Recommendations *bool     `toml:"recommendations"`
	SSE             *fileSSE  `toml:"sse"`
	CORS            *fileCORS `toml:"cors"`
	OperationsLevel *int      `toml:"operations_level"`
	BasePath        *string   `toml:"base_path"`
	TrustedProxies  *string   `toml:"trusted_proxies"`
}

type fileCORS struct {
	Origins []string `toml:"origins"`
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

type fileACL struct {
	Policy              *string `toml:"policy"`
	PolicyFile          *string `toml:"policy_file"`
	TailscaleCapability *string `toml:"tailscale_capability"`
	OIDCClaim           *string `toml:"oidc_claim"`
	HeadersACL          *string `toml:"headers_acl"`
	Labels              *bool   `toml:"labels"`
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
