package mcp

import (
	"context"
	"fmt"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// serviceRemoval is the confirmation remove_service asks for.
func (s *Server) serviceRemoval(_ context.Context, arg string) (confirmation, error) {
	svc, found, err := s.cache.ResolveService(arg)
	if err != nil {
		return confirmation{}, err
	}

	if !found {
		return confirmation{}, fmt.Errorf("service %q not found", arg)
	}

	impact := cluster.ServiceRemovalImpact(svc, s.cache.RunningTaskCount(svc.ID))

	return confirmation{
		target:   "service:" + svc.Spec.Name,
		message:  impact.Message(),
		version:  svc.Version.Index,
		bind:     svc.ID,
		declined: removalResult{Removed: false},
	}, nil
}
