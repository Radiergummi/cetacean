package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/attestations"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// attestationResource splits a sub-resource such as `sbom?platform=linux/arm64`
// into its name and the platform it asks for, and reports whether it is one of
// the attestation resources.
func attestationResource(subResource string) (name, platform string, ok bool) {
	name, query, _ := strings.Cut(subResource, "?")

	switch name {
	case "provenance", "sbom", "dockerfile":
		values, _ := url.ParseQuery(query)

		return name, values.Get("platform"), true
	default:
		return "", "", false
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
	var node *swarm.Node
	if n, ok := s.cache.GetNode(task.NodeID); ok {
		node = &n
	}

	if name == "provenance" {
		return cluster.TaskProvenance(ctx, task, node, s.attestations), nil
	}

	return s.readAttestation(ctx, cluster.TaskAttestationTarget(ctx, task, node, s.attestations),
		name, platform)
}

// readAttestation reads one platform's SBOM, as its package table rather than
// the SPDX document, or its Dockerfile.
func (s *Server) readAttestation(
	ctx context.Context,
	target cluster.AttestationTarget,
	name, requested string,
) (any, error) {
	if status := target.Attested.Status; status != attestations.StatusAvailable {
		return nil, fmt.Errorf("the image's attestations are %s", status)
	}

	platform, err := cluster.SelectPlatform(target.Candidates, requested)
	if err != nil {
		names := make([]string, len(target.Candidates))
		for i, p := range target.Candidates {
			names[i] = p.String()
		}

		if errors.Is(err, cluster.ErrPlatformRequired) {
			return nil, fmt.Errorf(
				"name one platform of %s as ?platform=",
				strings.Join(names, ", "),
			)
		}

		return nil, fmt.Errorf(
			"no platform %q; this image has %s",
			requested,
			strings.Join(names, ", "),
		)
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
