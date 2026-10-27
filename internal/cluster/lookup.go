package cluster

import (
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// Record is one resource as a lookup finds it: the ID it is keyed by, the name
// it is authorized under, and its version, zero for a kind Docker does not
// version.
type Record struct {
	Kind    string
	ID      string
	Name    string
	Version uint64

	write string
}

// Resource is the ACL resource a read of the record is checked against.
func (r Record) Resource() string { return r.Kind + ":" + r.Name }

// WriteResource is the ACL resource a write to the record is checked against.
func (r Record) WriteResource() string {
	if r.write != "" {
		return r.write
	}

	return r.Resource()
}

// Lookup resolves ident, an ID or a name, to the record of kind it addresses.
// It answers a task's "<service>.<slot>" name for anyone, so a transport
// resolving one for a caller uses LookupAs.
func Lookup(c *cache.Cache, kind, ident string) (Record, bool, error) {
	return LookupAs(c, kind, ident, nil)
}

// LookupAs is Lookup for a caller reading through canRead; nil reads all.
func LookupAs(
	c *cache.Cache,
	kind, ident string,
	canRead func(resource string) bool,
) (Record, bool, error) {
	switch kind {
	case "service":
		svc, found, err := c.ResolveService(ident)

		return Record{Kind: kind, ID: svc.ID, Name: svc.Spec.Name, Version: svc.Version.Index},
			found, err
	case "node":
		node, found, err := c.ResolveNode(ident)
		if !found {
			return Record{}, false, err
		}

		return NodeRecord(node), true, nil
	case "task":
		task, found, err := ResolveTaskAs(c, ident, canRead)
		if !found {
			return Record{}, false, err
		}

		return TaskRecord(c, task), true, nil
	case "config":
		cfg, found, err := c.ResolveConfig(ident)

		return Record{Kind: kind, ID: cfg.ID, Name: cfg.Spec.Name, Version: cfg.Version.Index},
			found, err
	case "secret":
		sec, found, err := c.ResolveSecret(ident)

		return Record{Kind: kind, ID: sec.ID, Name: sec.Spec.Name, Version: sec.Version.Index},
			found, err
	case "network":
		net, found, err := c.ResolveNetwork(ident)

		return Record{Kind: kind, ID: net.ID, Name: net.Name}, found, err
	case "stack":
		_, found := c.GetStack(ident)

		return keyedByName(kind, ident, found), found, nil
	case "volume":
		_, found := c.GetVolume(ident)

		return keyedByName(kind, ident, found), found, nil
	default:
		return Record{}, false, nil
	}
}

func keyedByName(kind, name string, found bool) Record {
	if !found {
		return Record{}
	}

	return Record{Kind: kind, ID: name, Name: name}
}

// NodeRecord is the record of a node already in hand.
func NodeRecord(node swarm.Node) Record {
	return Record{Kind: "node", ID: node.ID, Name: NodeACLName(node), Version: node.Version.Index}
}

// TaskRecord is the record of a task already in hand. A task is written as its
// service, and as itself only once that service is gone: grants name services,
// and a vanished one has no name left to match.
func TaskRecord(c *cache.Cache, task swarm.Task) Record {
	record := Record{Kind: "task", ID: task.ID, Name: task.ID, Version: task.Version.Index}

	if svc, ok := c.GetService(task.ServiceID); ok && svc.Spec.Name != "" {
		record.write = "service:" + svc.Spec.Name
	}

	return record
}
