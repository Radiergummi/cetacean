package cluster

import (
	"maps"
	"slices"

	"github.com/radiergummi/cetacean/internal/config"
)

// Operation is a write both transports expose. Its operations tier is stated
// here once, and the REST routes and MCP tools carrying it out read it.
type Operation string

const (
	OpScaleService         Operation = "service.scale"
	OpUpdateServiceImage   Operation = "service.image"
	OpRollbackService      Operation = "service.rollback"
	OpRestartService       Operation = "service.restart"
	OpUpdateService        Operation = "service.update"
	OpUpdateServiceSecrets Operation = "service.secrets"
	OpUpdateServiceConfigs Operation = "service.configs"
	OpUpdateServiceMounts  Operation = "service.mounts"
	OpRemoveService        Operation = "service.remove"
	OpUpdateNodeLabels     Operation = "node.labels"
	OpUpdateNode           Operation = "node.update"
	OpRemoveTask           Operation = "task.remove"
	OpCreateConfig         Operation = "config.create"
	OpRemoveConfig         Operation = "config.remove"
	OpCreateSecret         Operation = "secret.create"
	OpRemoveSecret         Operation = "secret.remove"
	OpRemoveNetwork        Operation = "network.remove"
	OpRemoveVolume         Operation = "volume.remove"
)

var operationTiers = map[Operation]config.OperationsLevel{
	OpScaleService:         config.OpsOperational,
	OpUpdateServiceImage:   config.OpsOperational,
	OpRollbackService:      config.OpsOperational,
	OpRestartService:       config.OpsOperational,
	OpUpdateService:        config.OpsConfiguration,
	OpUpdateServiceSecrets: config.OpsConfiguration,
	OpUpdateServiceConfigs: config.OpsConfiguration,
	OpUpdateServiceMounts:  config.OpsConfiguration,
	OpRemoveService:        config.OpsImpactful,
	OpUpdateNodeLabels:     config.OpsConfiguration,
	OpUpdateNode:           config.OpsImpactful,
	OpRemoveTask:           config.OpsImpactful,
	OpCreateConfig:         config.OpsConfiguration,
	OpRemoveConfig:         config.OpsImpactful,
	OpCreateSecret:         config.OpsConfiguration,
	OpRemoveSecret:         config.OpsImpactful,
	OpRemoveNetwork:        config.OpsImpactful,
	OpRemoveVolume:         config.OpsImpactful,
}

// Tier is the operations level op requires. An operation missing from the
// table requires the highest, so an omission fails closed.
func (op Operation) Tier() config.OperationsLevel {
	if level, ok := operationTiers[op]; ok {
		return level
	}

	return config.OpsImpactful
}

// Operations lists every operation, sorted.
func Operations() []Operation {
	return slices.Sorted(maps.Keys(operationTiers))
}
