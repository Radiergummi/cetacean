package cluster

import (
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// FilterStackDetail drops the members the identity may not read.
//
// A stack grant used to imply its members, because membership comes from a
// label the policy could name. A resource label narrows per resource and
// breaks that: the stack itself carries no ACL label, so a grant on it would
// otherwise hand back a service the label deliberately withholds.
func FilterStackDetail(
	e *acl.Evaluator,
	id *auth.Identity,
	d cache.StackDetail,
) cache.StackDetail {
	d.Services = acl.Filter(e, id, "read", d.Services,
		func(s swarm.Service) string { return "service:" + s.Spec.Name })
	d.Configs = acl.Filter(e, id, "read", d.Configs,
		func(c swarm.Config) string { return "config:" + c.Spec.Name })
	d.Secrets = acl.Filter(e, id, "read", d.Secrets,
		func(s swarm.Secret) string { return "secret:" + s.Spec.Name })
	d.Networks = acl.Filter(e, id, "read", d.Networks,
		func(n network.Summary) string { return "network:" + n.Name })
	d.Volumes = acl.Filter(e, id, "read", d.Volumes,
		func(v volume.Volume) string { return "volume:" + v.Name })

	return d
}
