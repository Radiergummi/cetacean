package recommendations

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
)

// ClusterChecker implements Checker for cluster topology recommendations.
type ClusterChecker struct {
	cache *cache.Cache
}

// NewClusterChecker creates a new cluster checker.
func NewClusterChecker(c *cache.Cache) *ClusterChecker {
	return &ClusterChecker{cache: c}
}

func (cc *ClusterChecker) Name() string            { return "cluster" }
func (cc *ClusterChecker) Interval() time.Duration { return 60 * time.Second }

// Check inspects the cluster topology for single-replica services, manager nodes
// accepting workloads, and uneven task distribution.
func (cc *ClusterChecker) Check(_ context.Context) []Recommendation {
	var recs []Recommendation

	// Single-replica services
	for _, svc := range cc.cache.ListServices() {
		if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil && *svc.Spec.Mode.Replicated.Replicas == 1 {
			suggested := ptr(2)
			recs = append(recs, Recommendation{
				Category:   CategorySingleReplica,
				Severity:   SeverityInfo,
				Scope:      ScopeService,
				TargetID:   svc.ID,
				TargetName: svc.Spec.Name,
				Message:    "Service has only 1 replica (no redundancy)",
				Suggested:  suggested,
				FixAction:  strPtr("PUT /services/{id}/scale"),
			})
		}
	}

	// Manager nodes with active availability
	for _, node := range cc.cache.ListNodes() {
		if node.Spec.Role == swarm.NodeRoleManager && node.Spec.Availability == swarm.NodeAvailabilityActive {
			recs = append(recs, Recommendation{
				Category:   CategoryManagerHasWorkloads,
				Severity:   SeverityWarning,
				Scope:      ScopeNode,
				TargetID:   node.ID,
				TargetName: node.Description.Hostname,
				Message:    "Manager node has active availability (may run workloads)",
				FixAction:  strPtr("PUT /nodes/{id}/availability"),
			})
		}
	}

	// Uneven task distribution
	taskCountByNode := make(map[string]int)
	for _, task := range cc.cache.ListTasks() {
		if task.Status.State == swarm.TaskStateRunning {
			taskCountByNode[task.NodeID]++
		}
	}

	if len(taskCountByNode) >= 2 {
		maxTasks := 0
		minTasks := int(^uint(0) >> 1) // max int

		for _, count := range taskCountByNode {
			if count > maxTasks {
				maxTasks = count
			}

			if count < minTasks {
				minTasks = count
			}
		}

		if minTasks > 0 && maxTasks/minTasks > 3 {
			recs = append(recs, Recommendation{
				Category: CategoryUnevenDistribution,
				Severity: SeverityInfo,
				Scope:    ScopeCluster,
				Message:  fmt.Sprintf("Task distribution is uneven: %d tasks on busiest node vs %d on least busy", maxTasks, minTasks),
			})
		}
	}

	return recs
}
