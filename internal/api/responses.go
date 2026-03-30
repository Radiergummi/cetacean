package api

import (
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/recommendations"
	"github.com/radiergummi/cetacean/internal/version"
)

// --- Core resource detail responses ---

// NodeResponse is the extra payload for GET /nodes/{id}.
type NodeResponse struct {
	Node swarm.Node `json:"node"`
}

// ServiceResponse is the extra payload for GET /services/{id}.
// Changes and Integrations are populated only when present.
type ServiceResponse struct {
	Service      swarm.Service `json:"service"`
	Changes      []SpecChange  `json:"changes,omitempty"`
	Integrations []any         `json:"integrations,omitempty"`
}

// TaskResponse is the extra payload for GET /tasks/{id}.
type TaskResponse struct {
	Task    EnrichedTask `json:"task"`
	Service TaskServiceRef `json:"service"`
	Node    TaskNodeRef    `json:"node"`
}

// TaskServiceRef is a JSON-LD cross-reference to a service within a task detail.
type TaskServiceRef struct {
	AtID string `json:"@id"`
	Name string `json:"name"`
}

// TaskNodeRef is a JSON-LD cross-reference to a node within a task detail.
type TaskNodeRef struct {
	AtID     string `json:"@id"`
	Hostname string `json:"hostname"`
}

// ConfigResponse is the extra payload for GET /configs/{id}.
type ConfigResponse struct {
	Config   swarm.Config     `json:"config"`
	Services []cache.ServiceRef `json:"services"`
}

// SecretResponse is the extra payload for GET /secrets/{id}.
type SecretResponse struct {
	Secret   swarm.Secret     `json:"secret"`
	Services []cache.ServiceRef `json:"services"`
}

// NetworkResponse is the extra payload for GET /networks/{id}.
type NetworkResponse struct {
	Network  network.Summary  `json:"network"`
	Services []cache.ServiceRef `json:"services"`
}

// VolumeResponse is the extra payload for GET /volumes/{name}.
type VolumeResponse struct {
	Volume   volume.Volume    `json:"volume"`
	Services []cache.ServiceRef `json:"services"`
}

// StackResponse is the extra payload for GET /stacks/{name}.
type StackResponse struct {
	Stack cache.StackDetail `json:"stack"`
}

// PluginResponse is the extra payload for GET /plugins/{name}.
type PluginResponse struct {
	Plugin types.Plugin `json:"plugin"`
}

// SwarmResponse is the extra payload for GET /swarm.
type SwarmResponse struct {
	Swarm       swarm.Swarm `json:"swarm"`
	ManagerAddr string      `json:"managerAddr"`
}

// --- Cluster and dashboard responses ---

// ClusterOverviewResponse is the extra payload for GET /cluster.
type ClusterOverviewResponse struct {
	NodeCount            int            `json:"nodeCount"`
	ServiceCount         int            `json:"serviceCount"`
	TaskCount            int            `json:"taskCount"`
	StackCount           int            `json:"stackCount"`
	TasksByState         map[string]int `json:"tasksByState"`
	NodesReady           int            `json:"nodesReady"`
	NodesDown            int            `json:"nodesDown"`
	NodesDraining        int            `json:"nodesDraining"`
	ServicesConverged    int            `json:"servicesConverged"`
	ServicesDegraded     int            `json:"servicesDegraded"`
	ReservedCPU          int64          `json:"reservedCPU"`
	ReservedMemory       int64          `json:"reservedMemory"`
	TotalCPU             int            `json:"totalCPU"`
	TotalMemory          int64          `json:"totalMemory"`
	PrometheusConfigured bool           `json:"prometheusConfigured"`
	LocalNodeID          string         `json:"localNodeID,omitempty"`
}

// ClusterCapacityResponse is the extra payload for GET /cluster/capacity.
type ClusterCapacityResponse struct {
	MaxNodeCPU    int   `json:"maxNodeCPU"`
	MaxNodeMemory int64 `json:"maxNodeMemory"`
	TotalCPU      int   `json:"totalCPU"`
	TotalMemory   int64 `json:"totalMemory"`
	NodeCount     int   `json:"nodeCount"`
}

// SearchResponse is the extra payload for GET /search.
type SearchResponse struct {
	Query   string                       `json:"query"`
	Results map[string][]searchResult    `json:"results"`
	Counts  map[string]int               `json:"counts"`
	Total   int                          `json:"total"`
}

// RecommendationsResponse is the extra payload for GET /recommendations.
type RecommendationsResponse struct {
	Items      []recommendations.Recommendation `json:"items"`
	Total      int                              `json:"total"`
	Summary    recommendations.Summary          `json:"summary"`
	ComputedAt time.Time                        `json:"computedAt"`
}

// HealthResponse is the body for GET /-/health.
type HealthResponse struct {
	Status          string                 `json:"status"`
	Version         string                 `json:"version"`
	Commit          string                 `json:"commit"`
	BuildDate       string                 `json:"buildDate"`
	OperationsLevel config.OperationsLevel `json:"operationsLevel"`
}

// --- Sub-resource responses ---

// LabelsResponse wraps a labels map for sub-resource endpoints.
type LabelsResponse struct {
	Labels map[string]string `json:"labels"`
}

// EnvResponse wraps an env map for GET /services/{id}/env.
type EnvResponse struct {
	Env map[string]string `json:"env"`
}

// NodeRoleResponse is the body for GET /nodes/{id}/role.
type NodeRoleResponse struct {
	Role         string `json:"role"`
	IsLeader     bool   `json:"isLeader"`
	ManagerCount int    `json:"managerCount"`
}

// --- Helpers to build HealthResponse from global state ---

// NewHealthResponse builds a HealthResponse from the current version info.
func NewHealthResponse(status string, level config.OperationsLevel) HealthResponse {
	return HealthResponse{
		Status:          status,
		Version:         version.Version,
		Commit:          version.Commit,
		BuildDate:       version.Date,
		OperationsLevel: level,
	}
}
