package config

import "fmt"

type TLSConfig struct {
	Cert string // path to TLS certificate file
	Key  string // path to TLS private key file
}

func (c TLSConfig) Enabled() bool {
	return c.Cert != "" || c.Key != ""
}

func LoadTLS(flags *Flags, fc *fileConfig) TLSConfig {
	if flags == nil {
		flags = &Flags{}
	}
	var ft *fileTLS
	if fc != nil {
		ft = fc.TLS
	}
	return TLSConfig{
		Cert: resolve(flags.TLSCert, "CETACEAN_TLS_CERT", fileField(ft, func(t *fileTLS) *string { return t.Cert }), ""),
		Key:  resolve(flags.TLSKey, "CETACEAN_TLS_KEY", fileField(ft, func(t *fileTLS) *string { return t.Key }), ""),
	}
}

func ValidateTLS(cfg TLSConfig) error {
	if (cfg.Cert == "") != (cfg.Key == "") {
		return fmt.Errorf("both CETACEAN_TLS_CERT and CETACEAN_TLS_KEY must be set, or neither")
	}
	return nil
}
