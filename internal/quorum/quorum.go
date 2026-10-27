// Package quorum reads a swarm's Raft quorum from its nodes. It is a leaf so
// the domain layer and the recommendations engine share one reading of it.
package quorum

import "github.com/docker/docker/api/types/swarm"

// Quorum is the manager picture: how many managers there are, how many are
// reachable, how many must be for the swarm to accept changes, and how many
// more reachable managers it can lose before it can't.
type Quorum struct {
	Managers  int `json:"managers"`
	Reachable int `json:"reachable"`
	Size      int `json:"size"`
	CanLose   int `json:"canLose"`
}

// Of counts the managers among nodes.
func Of(nodes []swarm.Node) Quorum {
	var q Quorum

	for _, n := range nodes {
		if n.Spec.Role != swarm.NodeRoleManager {
			continue
		}

		q.Managers++

		if Reachable(n) {
			q.Reachable++
		}
	}

	if q.Managers == 0 {
		return q
	}

	q.Size = Size(q.Managers)
	q.CanLose = max(q.Reachable-q.Size, 0)

	return q
}

// Held reports whether enough managers are reachable to accept changes.
func (q Quorum) Held() bool { return q.Reachable >= q.Size }

// Size is the majority of managers Raft needs.
func Size(managers int) int { return managers/2 + 1 }

// Reachable reports whether a node is a manager its peers can reach.
func Reachable(n swarm.Node) bool {
	return n.ManagerStatus != nil && n.ManagerStatus.Reachability == swarm.ReachabilityReachable
}
