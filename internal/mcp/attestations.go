package mcp

import (
	"context"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// isAttestationResource reports whether a sub-resource is one of the image
// attestation views.
func isAttestationResource(subResource string) bool {
	switch subResource {
	case "provenance", "sbom", "dockerfile":
		return true
	default:
		return false
	}
}

func (s *Server) readServiceAttestation(
	ctx context.Context,
	svc swarm.Service,
	name, platform string,
) (any, error) {
	if name == "provenance" {
		return cluster.ServiceProvenance(
			ctx, svc, s.cache.ListTasksByService(svc.ID), s.cache.GetNode, s.attestations,
		), nil
	}

	return s.readAttestation(ctx, cluster.ServiceAttestationTarget(ctx, svc, s.attestations),
		name, platform)
}

func (s *Server) readTaskAttestation(
	ctx context.Context,
	task swarm.Task,
	name, platform string,
) (any, error) {
	if name == "provenance" {
		return cluster.TaskProvenance(ctx, task, s.cache.GetNode, s.attestations), nil
	}

	return s.readAttestation(
		ctx, cluster.TaskAttestationTarget(ctx, task, s.cache.GetNode, s.attestations),
		name, platform,
	)
}

// readAttestation reads one platform's SBOM, as its package table rather than
// the SPDX document, or its Dockerfile.
func (s *Server) readAttestation(
	ctx context.Context,
	target cluster.AttestationTarget,
	name, requested string,
) (any, error) {
	platform, err := target.Platform(requested)
	if err != nil {
		return nil, err
	}

	if name == "sbom" {
		sbom, _, err := s.attestations.SBOM(ctx, target.Image, platform)
		if err != nil {
			return nil, err
		}

		return cluster.NewSBOMView(platform, sbom), nil
	}

	d, err := s.attestations.Dockerfile(ctx, target.Image, platform)
	if err != nil {
		return nil, err
	}

	return cluster.DockerfileView{Platform: platform.String(), Dockerfile: d}, nil
}
