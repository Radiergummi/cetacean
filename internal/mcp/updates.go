package mcp

import (
	"context"
	"fmt"
	"slices"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// The section names the spec-editing tools report, and the keys of the
// cluster.ServiceDetails / NodeDigest projection each one covers.
//
// A section is answered from the same projection describe builds rather than
// from a second one written beside it, so a caller who edits a service's ports
// and then describes it is told the same thing twice. That is also what keeps
// the answer free of what these tools must never hand back: the projection
// reports environment variable and log-driver option *names*, never their
// values, and every one of these tools used to return the whole swarm.Service
// — so a call that only raised a CPU limit came back carrying the service's
// database password.
const (
	sectionEnv            = "env"
	sectionLabels         = "labels"
	sectionResources      = "resources"
	sectionPlacement      = "placement"
	sectionPorts          = "ports"
	sectionUpdatePolicy   = "update-policy"
	sectionRollbackPolicy = "rollback-policy"
	sectionLogDriver      = "log-driver"
	sectionHealthcheck    = "healthcheck"
	sectionCommand        = "command"
	sectionSecrets        = "secrets"
	sectionConfigs        = "configs"
	sectionAvailability   = "availability"
	sectionRole           = "role"
)

// serviceSectionKeys maps a section to the cluster.ServiceDetails keys that
// answer it. A key absent from the projection — an unset resource limit, a
// service with no ports — is simply absent from the result, which is the same
// thing describe reports and is how "there is nothing set here" is said.
var serviceSectionKeys = map[string][]string{
	sectionEnv:    {"envNames"},
	sectionLabels: {"labels"},
	sectionResources: {
		"cpuLimitCores",
		"memoryLimitBytes",
		"cpuReservationCores",
		"memoryReservationBytes",
	},
	sectionPlacement:      {"placementConstraints"},
	sectionPorts:          {"ports"},
	sectionUpdatePolicy:   {"updatePolicy"},
	sectionRollbackPolicy: {"rollbackPolicy"},
	sectionLogDriver:      {"logDriver"},
	sectionHealthcheck: {
		"healthcheck",
		"healthcheckTest",
		"healthcheckInterval",
		"healthcheckTimeout",
		"healthcheckStartPeriod",
		"healthcheckRetries",
	},
	sectionCommand: {"command", "args"},
	sectionSecrets: {"secretNames"},
	sectionConfigs: {"configNames"},
}

// updateServiceSections is the set of sections update_service dispatches, and
// is deliberately narrower than serviceSectionKeys.
//
// The two are not the same question. serviceSectionKeys answers "how is this
// section reported", and the attachment editors need an entry there because
// they reply through serviceUpdate like everything else. Whether update_service
// can *perform* the edit is a separate matter: an attachment editor takes a
// list of resource references, resolves each one and checks a read grant on
// it, which is a different argument shape and a different authorization step
// — so it is a tool of its own.
//
// Validating against the projection table conflated the two. update_service
// accepted section "secrets", reached no case in the switch, and answered with
// a zero-valued result that reads as a write that landed.
var updateServiceSections = []string{
	sectionEnv,
	sectionLabels,
	sectionResources,
	sectionPlacement,
	sectionPorts,
	sectionUpdatePolicy,
	sectionRollbackPolicy,
	sectionLogDriver,
	sectionHealthcheck,
	sectionCommand,
}

// nodeSectionKeys is the node counterpart, over NodeDigest's details.
var nodeSectionKeys = map[string][]string{
	sectionLabels:       {"labels"},
	sectionAvailability: {"availability"},
	sectionRole:         {"role"},
}

// serviceUpdateResult is what the spec-editing service tools return: enough to
// confirm the edit landed and to address the service again, and nothing else.
//
// Version is here because a follow-up write needs it — Docker rejects an
// update carrying a stale version, and reading it back off a cache the watcher
// fills asynchronously is a race these tools would otherwise force on every
// caller. It comes off the service the write returned, which docker.Client
// ends with a fresh inspect.
type serviceUpdateResult struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Version uint64         `json:"version"`
	Section string         `json:"section"`
	Details map[string]any `json:"details"`
}

// nodeUpdateResult is the node counterpart. Role and availability ride along
// whichever section was edited, because they are the two facts that decide
// what a node will accept next: draining a node to move its work is a
// different act on a manager than on a worker, and the answer to "did the
// drain take" is meaningless without the role it took effect on.
type nodeUpdateResult struct {
	ID           string         `json:"id"`
	Hostname     string         `json:"hostname"`
	Version      uint64         `json:"version"`
	Section      string         `json:"section"`
	Role         string         `json:"role"`
	Availability string         `json:"availability"`
	Details      map[string]any `json:"details"`
}

// serviceUpdate projects the service a write returned down to the section that
// was edited.
func serviceUpdate(section string, svc swarm.Service) serviceUpdateResult {
	return serviceUpdateResult{
		ID:      svc.ID,
		Name:    svc.Spec.Name,
		Version: svc.Version.Index,
		Section: section,
		Details: selectKeys(cluster.ServiceDetails(svc), serviceSectionKeys[section]),
	}
}

// nodeUpdate is the node counterpart. It reads the section's detail out of the
// digest projection rather than off the spec directly, for the same reason
// serviceUpdate does: one description of a node, not two.
func nodeUpdate(section string, node swarm.Node) nodeUpdateResult {
	details := cluster.NodeDigest(node, nil, nil).Details

	return nodeUpdateResult{
		ID:           node.ID,
		Hostname:     node.Description.Hostname,
		Version:      node.Version.Index,
		Section:      section,
		Role:         stringDetail(details, "role"),
		Availability: stringDetail(details, "availability"),
		Details:      selectKeys(details, nodeSectionKeys[section]),
	}
}

// selectKeys copies the named keys that are present. The result is never nil:
// an empty object says "this section is set to nothing", which a null could
// not distinguish from "this tool forgot to answer".
func selectKeys(details map[string]any, keys []string) map[string]any {
	out := make(map[string]any, len(keys))

	for _, key := range keys {
		if value, ok := details[key]; ok {
			out[key] = value
		}
	}

	return out
}

func stringDetail(details map[string]any, key string) string {
	value, _ := details[key].(string)

	return value
}

// toolUpdateService dispatches one section of a service's specification to the
// writer that owns it.
//
// The eight sections were eight tools until they were folded into this one.
// They shared a tier, an ACL check and a result shape, and differed only in
// which field of the spec they replaced — while each one spent about a
// kilobyte of every tools/list, over two thousand tokens of an agent's context
// before it had read a single service.
//
// The cost of folding is the input schema: `value` is whatever the section
// takes, so JSON Schema cannot describe it and the section's own decoder is
// what validates it. That is why decodeSection reports the section it was
// decoding and the shape it wanted — a model that guessed the payload wrong
// has to be told which of eight shapes it was being held to.
func (s *Server) toolUpdateService(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}

	section, err := req.RequireString("section")
	if err != nil {
		return "", err
	}

	if !slices.Contains(updateServiceSections, section) {
		// A section this tool does not dispatch but the projection knows
		// about is reached by a tool of its own, and the error is the only
		// place a caller who guessed finds out which one.
		if _, ok := serviceSectionKeys[section]; ok {
			return "", fmt.Errorf(
				"section %q is not edited through update_service; "+
					"use the update_service_%s tool, which takes a list of references",
				section, section,
			)
		}

		return "", fmt.Errorf(
			"unknown section %q; expected one of %v",
			section, updateServiceSections,
		)
	}

	// Before the write client is even resolved, for the same reason every
	// other mutation checks first: a refusal must not depend on whether
	// Docker happens to be reachable.
	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}

	writeClient, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}

	var updated swarm.Service

	switch section {
	case sectionEnv:
		patch, patchErr := requireStringMapPatch(req, "value")
		if patchErr != nil {
			return "", patchErr
		}

		updated, err = writeClient.UpdateServiceEnv(ctx, id, mergePatchMutator(patch))

	case sectionLabels:
		patch, patchErr := requireStringMapPatch(req, "value")
		if patchErr != nil {
			return "", patchErr
		}

		updated, err = writeClient.UpdateServiceLabels(ctx, id, mergePatchMutator(patch))

	case sectionResources:
		resources, decodeErr := decodeSection[swarm.ResourceRequirements](req, section)
		if decodeErr != nil {
			return "", decodeErr
		}

		updated, err = writeClient.UpdateServiceResources(ctx, id, &resources)

	case sectionPlacement:
		placement, decodeErr := decodeSection[swarm.Placement](req, section)
		if decodeErr != nil {
			return "", decodeErr
		}

		updated, err = writeClient.UpdateServicePlacement(ctx, id, &placement)

	case sectionPorts:
		ports, decodeErr := decodeSection[[]swarm.PortConfig](req, section)
		if decodeErr != nil {
			return "", decodeErr
		}

		updated, err = writeClient.UpdateServicePorts(ctx, id, ports)

	case sectionUpdatePolicy:
		policy, decodeErr := decodeSection[swarm.UpdateConfig](req, section)
		if decodeErr != nil {
			return "", decodeErr
		}

		updated, err = writeClient.UpdateServiceUpdatePolicy(ctx, id, &policy)

	case sectionRollbackPolicy:
		policy, decodeErr := decodeSection[swarm.UpdateConfig](req, section)
		if decodeErr != nil {
			return "", decodeErr
		}

		updated, err = writeClient.UpdateServiceRollbackPolicy(ctx, id, &policy)

	case sectionLogDriver:
		driver, decodeErr := decodeSection[swarm.Driver](req, section)
		if decodeErr != nil {
			return "", decodeErr
		}

		updated, err = writeClient.UpdateServiceLogDriver(ctx, id, &driver)

	case sectionHealthcheck:
		value, decodeErr := decodeSection[healthcheckValue](req, section)
		if decodeErr != nil {
			return "", decodeErr
		}

		hc, convErr := value.toHealthConfig()
		if convErr != nil {
			return "", convErr
		}

		updated, err = writeClient.UpdateServiceHealthcheck(ctx, id, hc)

	case sectionCommand:
		value, decodeErr := decodeSection[commandValue](req, section)
		if decodeErr != nil {
			return "", decodeErr
		}

		updated, err = writeClient.UpdateServiceContainerConfig(ctx, id, value.applyTo)
	}

	if err != nil {
		return "", err
	}

	return marshalResult(serviceUpdate(section, updated))
}

// toolUpdateNode is the node counterpart, over the two sections that need the
// impactful tier. Labels stay a tool of their own because they do not: folding
// them in would have raised the level a deployment needs to relabel a node to
// the level it needs to demote a manager.
func (s *Server) toolUpdateNode(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}

	section, err := req.RequireString("section")
	if err != nil {
		return "", err
	}

	value, err := req.RequireString("value")
	if err != nil {
		return "", err
	}

	if err := s.checkNodeWrite(ctx, id); err != nil {
		return "", err
	}

	writeClient, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}

	var updated swarm.Node

	switch section {
	case sectionAvailability:
		availability, parseErr := parseNodeAvailability(value)
		if parseErr != nil {
			return "", parseErr
		}

		updated, err = writeClient.UpdateNodeAvailability(ctx, id, availability)

	case sectionRole:
		role, parseErr := parseNodeRole(value)
		if parseErr != nil {
			return "", parseErr
		}

		updated, err = writeClient.UpdateNodeRole(ctx, id, role)

	default:
		return "", fmt.Errorf(
			"unknown section %q; expected %q or %q",
			section, sectionAvailability, sectionRole,
		)
	}

	if err != nil {
		return "", err
	}

	return marshalResult(nodeUpdate(section, updated))
}

// decodeSection decodes the `value` argument as the shape one section expects,
// naming both in the error. The folded tool cannot express eight payload
// shapes in one input schema, so this is where a wrong one is caught, and the
// message is all the model gets to work out which shape it should have sent.
func decodeSection[T any](req mcplib.CallToolRequest, section string) (T, error) {
	var out T

	if err := decodeArgInto(req, "value", &out); err != nil {
		return out, fmt.Errorf("section %q: %w", section, err)
	}

	return out, nil
}
