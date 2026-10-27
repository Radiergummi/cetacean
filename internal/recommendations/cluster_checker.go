package recommendations

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/ipam"
	"github.com/radiergummi/cetacean/internal/quorum"
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
// accepting workloads, the manager quorum, overlay networks running out of
// addresses, and uneven task distribution.
func (cc *ClusterChecker) Check(_ context.Context) []Recommendation {
	var recs []Recommendation

	services := cc.cache.ListServices()
	tasks := cc.cache.ListTasks()

	// Single-replica services
	for _, svc := range services {
		if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil &&
			*svc.Spec.Mode.Replicated.Replicas == 1 {
			recs = append(recs, Recommendation{
				Category:   CategorySingleReplica,
				Severity:   SeverityInfo,
				Scope:      ScopeService,
				TargetID:   svc.ID,
				TargetName: svc.Spec.Name,
				Message:    "Service has only 1 replica (no redundancy)",
				Suggested:  new(2.0),
				FixAction:  new("PUT /services/{id}/scale"),
			})
		}
	}

	// Manager nodes with active availability, when a worker could take their
	// tasks: without one, draining the managers strands everything.
	nodes := cc.cache.ListNodes()
	workerAvailable := false
	for _, node := range nodes {
		if node.Spec.Role == swarm.NodeRoleWorker &&
			node.Spec.Availability == swarm.NodeAvailabilityActive &&
			node.Status.State == swarm.NodeStateReady {
			workerAvailable = true
		}
	}
	for _, node := range nodes {
		if workerAvailable && node.Spec.Role == swarm.NodeRoleManager &&
			node.Spec.Availability == swarm.NodeAvailabilityActive {
			recs = append(recs, Recommendation{
				Category:   CategoryManagerHasWorkloads,
				Severity:   SeverityWarning,
				Scope:      ScopeNode,
				TargetID:   node.ID,
				TargetName: node.Description.Hostname,
				Message:    "Manager node has active availability (may run workloads)",
				FixAction:  new("PUT /nodes/{id}/availability"),
			})
		}
	}

	recs = append(recs, quorumRecommendations(nodes)...)

	recs = append(recs, addressRecommendations(
		cc.cache.ListNetworks(),
		cc.cache.NetworkAddresses(),
	)...)

	// Uneven task distribution
	taskCountByNode := make(map[string]int)
	for _, task := range tasks {
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
				Message: fmt.Sprintf(
					"Task distribution is uneven: %d tasks on busiest node vs %d on least busy",
					maxTasks,
					minTasks,
				),
			})
		}
	}

	return recs
}

// quorumRecommendations flags a manager set that tolerates fewer failures than
// the nodes allow. One manager is only flagged with two other ready nodes to
// promote, since a second manager alone tolerates no more failures than one.
func quorumRecommendations(nodes []swarm.Node) []Recommendation {
	q := quorum.Of(slices.Values(nodes))

	ready := 0
	for _, n := range nodes {
		if n.Status.State == swarm.NodeStateReady {
			ready++
		}
	}

	var recs []Recommendation

	cluster := func(category Category, severity Severity, message string) Recommendation {
		return Recommendation{
			Category: category,
			Severity: severity,
			Scope:    ScopeCluster,
			Message:  message,
		}
	}

	switch {
	case q.Managers == 1 && ready >= 3:
		recs = append(recs, cluster(CategorySingleManager, SeverityWarning,
			"Only 1 manager: losing it stops the swarm accepting changes"))
	case q.Managers >= 2 && q.Managers%2 == 0:
		recs = append(recs, cluster(CategoryEvenManagers, SeverityWarning, fmt.Sprintf(
			"%d managers tolerate no more failures than %d", q.Managers, q.Managers-1)))
	}

	switch {
	case q.Managers > 0 && !q.Held:
		recs = append(recs, cluster(CategoryQuorumAtRisk, SeverityCritical, fmt.Sprintf(
			"Quorum lost: %d of %d managers reachable, %d needed",
			q.Reachable,
			q.Managers,
			q.Size,
		)))
	case q.Reachable < q.Managers && q.CanLose == 0:
		recs = append(recs, cluster(CategoryQuorumAtRisk, SeverityCritical, fmt.Sprintf(
			"%d of %d managers reachable: one more loss stops the swarm accepting changes",
			q.Reachable, q.Managers)))
	case q.Reachable < q.Managers:
		recs = append(recs, cluster(CategoryQuorumAtRisk, SeverityWarning, fmt.Sprintf(
			"%d of %d managers reachable: quorum survives %d more %s",
			q.Reachable, q.Managers, q.CanLose, plural(q.CanLose, "loss", "losses"))))
	}

	return recs
}

// addressRecommendations flags a network with 80% of its addresses in use, and
// raises it to critical from 95%.
func addressRecommendations(
	networks []network.Summary,
	counts map[string]ipam.Addresses,
) []Recommendation {
	var recs []Recommendation

	for _, n := range networks {
		a, ok := counts[n.ID]
		if !ok || a.Usable == 0 || a.Used*100 < a.Usable*80 {
			continue
		}

		severity := SeverityWarning
		if a.Used*100 >= a.Usable*95 {
			severity = SeverityCritical
		}

		recs = append(recs, Recommendation{
			Category:   CategoryNetworkAddressesLow,
			Severity:   severity,
			Scope:      ScopeNetwork,
			TargetID:   n.ID,
			TargetName: n.Name,
			Message: fmt.Sprintf(
				"%d of %d addresses in use; a start-first rolling update needs one more per task it starts",
				a.Used,
				a.Usable,
			),
			Current:    new(float64(a.Used)),
			Configured: new(float64(a.Usable)),
		})
	}

	return recs
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}

	return many
}
