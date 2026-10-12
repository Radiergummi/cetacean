package cluster

import (
	"fmt"

	"github.com/docker/docker/api/types/swarm"
)

// ValidatePorts rejects a port that publishes nothing. Docker accepts a
// PortConfig with TargetPort 0, assigns an ephemeral published port pointing
// at container port 0, and reports success — so the service holds a port that
// cannot serve. The target is the one field with no sensible default.
func ValidatePorts(ports []swarm.PortConfig) error {
	for i, p := range ports {
		if p.TargetPort == 0 {
			return fmt.Errorf(
				"port %d: TargetPort is required and must not be 0 "+
					"(the container port to publish)", i,
			)
		}
	}

	return nil
}
