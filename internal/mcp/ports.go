package mcp

import (
	"context"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

type portsResult struct {
	Items []cache.PublishedPort `json:"items"`
	Total int                   `json:"total"`
}

// toolGetPublishedPorts returns the ports published by the services the caller
// may read, optionally narrowed to one published port.
func (s *Server) toolGetPublishedPorts(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	if err := s.requireAnyGrant(ctx); err != nil {
		return "", err
	}

	ports := acl.Filter(
		s.acl,
		auth.IdentityFromContext(ctx),
		"read",
		s.cache.ListPublishedPorts(),
		cluster.PortACLResource,
	)

	if port := req.GetInt("port", 0); port != 0 {
		matching := make([]cache.PublishedPort, 0, 1)

		for _, p := range ports {
			if int(p.PublishedPort) == port {
				matching = append(matching, p)
			}
		}

		ports = matching
	}

	if ports == nil {
		ports = []cache.PublishedPort{}
	}

	return marshalResult(portsResult{Items: ports, Total: len(ports)})
}
