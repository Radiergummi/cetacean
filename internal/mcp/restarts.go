package mcp

import (
	"time"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// restartWindows are the two the digest reports. The short one answers "is
// this flapping right now", the long one "has it always been"; the tracker's
// own horizon is 7d, so asking for more would report a floor, not a count.
const (
	restartWindowShort = time.Hour
	restartWindowLong  = 7 * 24 * time.Hour
)

// serviceRestarts reads the cache's restart tracker for one service.
//
// The tracker records every task transition into failed, rejected or orphaned
// and already survives restarts through the snapshot, so this costs two map
// reads and is the cheapest signal on the surface — which matters, because a
// digest is rebuilt on every cache event for a subscribed resource.
//
// It never returns nil: the cache always has a tracker, so a zero here means
// "measured, none", which is a real answer. The nil case belongs to callers
// that have no tracker at all.
func (s *Server) serviceRestarts(serviceID string) *cluster.ServiceRestarts {
	return &cluster.ServiceRestarts{
		LastHour:      s.cache.RestartCount(serviceID, restartWindowShort),
		LastWeek:      s.cache.RestartCount(serviceID, restartWindowLong),
		TrackingSince: s.cache.RestartTrackingSince().UTC().Format(time.RFC3339),
	}
}
