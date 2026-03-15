package config

import (
	"flag"
	"os"
)

// Flags holds parsed CLI flag values. Pointer fields distinguish
// "not set" (nil) from "set to zero value".
type Flags struct {
	Config        string // path to TOML config file
	Listen        *string
	DockerHost    *string
	PrometheusURL *string
	LogLevel      *string
	LogFormat     *string
	AuthMode      *string
	Pprof         *bool
	Version       bool
}

// ParseFlags parses CLI flags from args (typically os.Args[1:]).
// Only flags that were explicitly set get non-nil pointer values.
func ParseFlags(args []string) (*Flags, error) {
	fs := flag.NewFlagSet("cetacean", flag.ContinueOnError)

	var f Flags

	// -config can also come from CETACEAN_CONFIG env var.
	configDefault := os.Getenv("CETACEAN_CONFIG")
	fs.StringVar(&f.Config, "config", configDefault, "Path to TOML config file (env: CETACEAN_CONFIG)")

	listen := fs.String("listen", "", "Listen address (env: CETACEAN_LISTEN_ADDR, default \":9000\")")
	dockerHost := fs.String("docker-host", "", "Docker socket (env: CETACEAN_DOCKER_HOST)")
	prometheusURL := fs.String("prometheus-url", "", "Prometheus URL (env: CETACEAN_PROMETHEUS_URL)")
	logLevel := fs.String("log-level", "", "Log level (env: CETACEAN_LOG_LEVEL, default \"info\")")
	logFormat := fs.String("log-format", "", "Log format (env: CETACEAN_LOG_FORMAT, default \"json\")")
	authMode := fs.String("auth-mode", "", "Auth mode (env: CETACEAN_AUTH_MODE, default \"none\")")
	pprof := fs.Bool("pprof", false, "Enable pprof (env: CETACEAN_PPROF)")
	fs.BoolVar(&f.Version, "version", false, "Print version and exit")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	// Only set pointer fields for flags that were explicitly provided.
	fs.Visit(func(fl *flag.Flag) {
		switch fl.Name {
		case "listen":
			f.Listen = listen
		case "docker-host":
			f.DockerHost = dockerHost
		case "prometheus-url":
			f.PrometheusURL = prometheusURL
		case "log-level":
			f.LogLevel = logLevel
		case "log-format":
			f.LogFormat = logFormat
		case "auth-mode":
			f.AuthMode = authMode
		case "pprof":
			f.Pprof = pprof
		}
	})

	return &f, nil
}
