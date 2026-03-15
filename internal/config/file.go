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
}

type fileServer struct {
	ListenAddr *string  `toml:"listen_addr"`
	Pprof      *bool    `toml:"pprof"`
	SSE        *fileSSE `toml:"sse"`
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
