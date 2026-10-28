// Package core holds the state the REST and MCP transports share. Built once
// and handed to both, so neither can quietly run on a copy of its own.
package core

import (
	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// Core is what both transports read and write through. A nil Streams leaves
// streams to their clients.
type Core struct {
	Cache *cache.Cache

	// Versions groups configs and secrets, rebuilt once per cache generation.
	Versions *cluster.VersionIndex

	// SecretEntries is where MCP's create_secret leaves a secret for a person
	// to enter in the dashboard, which the REST create completes.
	SecretEntries *cluster.SecretEntries

	// LogReads is the pool plain log reads draw on in either transport.
	LogReads *sse.Slots

	// Streams ends the metrics stream, log follows and MCP subscriptions.
	Streams *sse.Lifetime
}

// New builds the shared state over c.
func New(c *cache.Cache, streams *sse.Lifetime) *Core {
	return &Core{
		Cache:         c,
		Versions:      cluster.NewVersionIndex(c),
		SecretEntries: cluster.NewSecretEntries(),
		LogReads:      new(sse.Slots),
		Streams:       streams,
	}
}
