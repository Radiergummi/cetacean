package mcp

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/mcp/projection"
)

// The section names the spec-editing tools report, and the keys of the
// projection.ServiceDetails / NodeDigest projection each covers. Answering from
// the same projection describe builds keeps an edit and a description in step,
// and keeps values out: it reports environment and log-driver option *names*.
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
	sectionMounts         = "mounts"
	sectionAvailability   = "availability"
	sectionRole           = "role"
)

// serviceSectionKeys maps a section to the projection.ServiceDetails keys that
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

	// Both keys: bindMounts reports the security-relevant mounts in full but
	// skips volumes, so it cannot on its own confirm a wholesale replacement
	// left the data volume in place.
	sectionMounts: {"mountTargets", "bindMounts"},
}

// sectionChange decodes one section's `value` into the change it makes.
// Splitting the decode out is not worth it: the shape `value` takes is
// exactly what decides which change runs.
type sectionChange func(req mcplib.CallToolRequest, section string) (cluster.SpecChange, error)

// serviceSectionChanges is what update_service can perform, deliberately
// narrower than serviceSectionKeys, which only answers how a section is
// reported. The accepted list, the advertised enum and the dispatch are one
// table, so a section absent here is refused before any write is attempted.
var serviceSectionChanges = map[string]sectionChange{
	sectionEnv: func(req mcplib.CallToolRequest, _ string) (cluster.SpecChange, error) {
		patch, err := requireStringMapPatch(req, "value")
		if err != nil {
			return nil, err
		}

		return cluster.PatchEnv(mergePatchMutator(patch)), nil
	},

	sectionLabels: func(req mcplib.CallToolRequest, _ string) (cluster.SpecChange, error) {
		patch, err := requireStringMapPatch(req, "value")
		if err != nil {
			return nil, err
		}

		return cluster.PatchLabels(mergePatchMutator(patch)), nil
	},

	sectionResources: func(req mcplib.CallToolRequest, section string) (cluster.SpecChange, error) {
		resources, err := decodeSection[swarm.ResourceRequirements](req, section)
		if err != nil {
			return nil, err
		}

		return cluster.SetResources(&resources), nil
	},

	sectionPlacement: func(req mcplib.CallToolRequest, section string) (cluster.SpecChange, error) {
		placement, err := decodeSection[swarm.Placement](req, section)
		if err != nil {
			return nil, err
		}

		return cluster.SetPlacement(&placement), nil
	},

	sectionPorts: func(req mcplib.CallToolRequest, section string) (cluster.SpecChange, error) {
		ports, err := decodeSection[[]swarm.PortConfig](req, section)
		if err != nil {
			return nil, err
		}

		if err := cluster.ValidatePorts(ports); err != nil {
			return nil, fmt.Errorf("section %q: %w", section, err)
		}

		return cluster.SetPorts(ports), nil
	},

	sectionUpdatePolicy: func(req mcplib.CallToolRequest, section string) (cluster.SpecChange, error) {
		policy, err := decodeSection[swarm.UpdateConfig](req, section)
		if err != nil {
			return nil, err
		}

		return cluster.SetUpdatePolicy(&policy), nil
	},

	sectionRollbackPolicy: func(req mcplib.CallToolRequest, section string) (cluster.SpecChange, error) {
		policy, err := decodeSection[swarm.UpdateConfig](req, section)
		if err != nil {
			return nil, err
		}

		return cluster.SetRollbackPolicy(&policy), nil
	},

	sectionLogDriver: func(req mcplib.CallToolRequest, section string) (cluster.SpecChange, error) {
		driver, err := decodeSection[swarm.Driver](req, section)
		if err != nil {
			return nil, err
		}

		return cluster.SetLogDriver(&driver), nil
	},

	sectionHealthcheck: func(req mcplib.CallToolRequest, section string) (cluster.SpecChange, error) {
		value, err := decodeSection[healthcheckValue](req, section)
		if err != nil {
			return nil, err
		}

		hc, err := value.toHealthConfig()
		if err != nil {
			return nil, err
		}

		return cluster.SetHealthcheck(hc), nil
	},

	sectionCommand: func(req mcplib.CallToolRequest, section string) (cluster.SpecChange, error) {
		value, err := decodeSection[commandValue](req, section)
		if err != nil {
			return nil, err
		}

		return cluster.EditContainer(func(cs *swarm.ContainerSpec) error {
			value.applyTo(cs)
			return nil
		}), nil
	},
}

// updateServiceSections is the advertised enum and the error message's list:
// the table's own keys, sorted so tools/list is byte-stable across runs.
var updateServiceSections = slices.Sorted(maps.Keys(serviceSectionChanges))

// nodeSectionKeys is the node counterpart, over NodeDigest's details.
var nodeSectionKeys = map[string][]string{
	sectionLabels:       {"labels"},
	sectionAvailability: {"availability"},
	sectionRole:         {"role"},
}

// serviceUpdateResult is what the spec-editing service tools return: enough to
// confirm the edit landed and address the service again. Version comes off the
// write's own inspect, as the asynchronously filled cache may not hold it yet;
// a rolling update the write started moves it on again.
type serviceUpdateResult struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Version uint64         `json:"version"`
	Section string         `json:"section"`
	Details map[string]any `json:"details"`
}

// nodeUpdateResult is the node counterpart. Role and availability ride along
// whichever section was edited: they decide what the node accepts next, and
// draining a manager to move its work is a different act from draining a worker.
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
		Details: selectKeys(projection.ServiceDetails(svc), serviceSectionKeys[section]),
	}
}

// nodeUpdate is the node counterpart. It reads the section's detail out of the
// digest projection rather than off the spec directly, for the same reason
// serviceUpdate does: one description of a node, not two.
func nodeUpdate(section string, node swarm.Node) nodeUpdateResult {
	details := projection.NodeDigest(node, nil, nil).Details

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
// change that owns it. The cost of folding eight tools into one is the input
// schema: `value` is whatever the section takes, so JSON Schema cannot describe
// it and decodeSection has to name both the section and the shape it wanted.
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

	decode, dispatched := serviceSectionChanges[section]
	if !dispatched {
		// A section this tool does not dispatch but the projection knows
		// about is reached by a tool of its own, and the error is the only
		// place a caller who guessed finds out which one.
		if _, ok := serviceSectionKeys[section]; ok {
			return "", fmt.Errorf(
				"section %q is not edited through update_service; "+
					"use the update_service_%s tool, which takes the whole list at once",
				section, section,
			)
		}

		return "", fmt.Errorf(
			"unknown section %q; expected one of %v",
			section, updateServiceSections,
		)
	}

	// The use case is the authority; this refuses early, so a value decoded
	// for a caller who may not write here never reaches the cache lookups.
	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}

	change, err := decode(req, section)
	if err != nil {
		return "", err
	}

	ctx, id, cond, err := s.pinVersion(ctx, req, "service", id)
	if err != nil {
		return "", err
	}

	if _, err := s.requireWriteClient(); err != nil {
		return "", err
	}

	updated, err := s.writes.UpdateService(
		ctx, s.writeCaller(ctx), cluster.OpUpdateService, id, change,
	)
	if err != nil {
		return "", s.refused(ctx, cond, err)
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

	var (
		availability swarm.NodeAvailability
		role         swarm.NodeRole
	)

	switch section {
	case sectionAvailability:
		availability, err = parseNodeAvailability(value)
	case sectionRole:
		role, err = parseNodeRole(value)
	default:
		err = fmt.Errorf(
			"unknown section %q; expected %q or %q",
			section, sectionAvailability, sectionRole,
		)
	}

	if err != nil {
		return "", err
	}

	id, err = s.resolveNodeWrite(ctx, req, id)
	if err != nil {
		return "", err
	}

	ctx, id, cond, err := s.pinVersion(ctx, req, "node", id)
	if err != nil {
		return "", err
	}

	confirmed, err := s.confirm(ctx, req, func() (confirmation, error) {
		return s.nodeChange(ctx, id, section, availability, role)
	})
	if err != nil {
		return "", err
	}

	writeClient, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}

	var updated swarm.Node

	if section == sectionRole {
		updated, err = writeClient.UpdateNodeRole(ctx, id, role)
	} else {
		updated, err = writeClient.UpdateNodeAvailability(ctx, id, availability)
	}

	if err != nil {
		return "", s.refused(ctx, cond, err)
	}

	logConfirmableWrite(ctx, req.Params.Name, "node:"+id, confirmed)

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
