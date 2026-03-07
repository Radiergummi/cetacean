package main

import (
	"log"
	"os"

	"cetacean/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	log.Printf("cetacean starting on %s", cfg.ListenAddr)
	log.Printf("docker: %s", cfg.DockerHost)
	log.Printf("prometheus: %s", cfg.PrometheusURL)

	// Components will be wired here in subsequent tasks.
	_ = cfg
	os.Exit(0)
}
