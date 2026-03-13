package config

import (
	"fmt"
	"os"
)

type TLSConfig struct {
	Cert string // path to TLS certificate file
	Key  string // path to TLS private key file
}

func (c TLSConfig) Enabled() bool {
	return c.Cert != "" || c.Key != ""
}

func LoadTLS() TLSConfig {
	return TLSConfig{
		Cert: os.Getenv("CETACEAN_TLS_CERT"),
		Key:  os.Getenv("CETACEAN_TLS_KEY"),
	}
}

func ValidateTLS(cfg TLSConfig) error {
	if (cfg.Cert == "") != (cfg.Key == "") {
		return fmt.Errorf("both CETACEAN_TLS_CERT and CETACEAN_TLS_KEY must be set, or neither")
	}
	return nil
}
