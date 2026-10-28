// Package quorum reads a swarm's Raft quorum from its nodes. It is a leaf so
// the domain layer and the recommendations engine share one reading of it.
package quorum

import (
	"iter"

	"github.com/docker/docker/api/types/swarm"
)

// Quorum is the manager picture: how many managers there are, how many are
// reachable, how many must be for the swarm to accept changes, whether they
// are, and how many more reachable managers it can lose before it can't.
type Quorum struct {
	Managers  int  `json:"managers"`
	Reachable int  `json:"reachable"`
	Size      int  `json:"size"`
	Held      bool `json:"held"`
	CanLose   int  `json:"canLose"`
}

// Of counts the Raft members among nodes: a node is a manager once it carries
// a ManagerStatus, whatever role it has been asked to take. Without managers,
// as before the first sync, nothing is held.
func Of(nodes iter.Seq[swarm.Node]) Quorum {
	var q Quorum

	for n := range nodes {
		if n.ManagerStatus == nil {
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
	q.Held = q.Reachable >= q.Size
	q.CanLose = max(q.Reachable-q.Size, 0)

	return q
}

// Size is the majority of managers Raft needs.
func Size(managers int) int { return managers/2 + 1 }

// Reachable reports whether a node is a manager its peers have not found
// unreachable. Docker reports "unknown" while an election settles, so only an
// explicit "unreachable" counts against quorum.
func Reachable(n swarm.Node) bool {
	return n.ManagerStatus != nil && n.ManagerStatus.Reachability != swarm.ReachabilityUnreachable
}
