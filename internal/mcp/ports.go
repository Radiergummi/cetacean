package mcp

import (
	"context"
	"fmt"
	"math"

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

	port, err := portArgument(req)
	if err != nil {
		return "", err
	}

	identity := auth.IdentityFromContext(ctx)
	canRead := func(resource string) bool { return s.canRead(identity, resource) }

	ports := acl.Filter(
		s.acl,
		identity,
		"read",
		cluster.PublishedPorts(s.cache, canRead),
		cluster.PortACLResource,
	)

	if port != 0 {
		matching := make([]cache.PublishedPort, 0, 1)

		for _, p := range ports {
			if p.PublishedPort == port {
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

// portArgument reads the optional `port`, refusing anything that isn't a port
// rather than answering for all of them, or for none, as though it were.
func portArgument(req mcplib.CallToolRequest) (uint32, error) {
	raw, present := req.GetArguments()["port"]
	if !present || raw == nil {
		return 0, nil
	}

	var number float64

	switch value := raw.(type) {
	case float64:
		number = value
	case int:
		number = float64(value)
	default:
		number = math.NaN()
	}

	if number != math.Trunc(number) || number < 1 || number > 65535 {
		return 0, fmt.Errorf("port must be a whole number from 1 to 65535; got %v", raw)
	}

	return uint32(number), nil
}
