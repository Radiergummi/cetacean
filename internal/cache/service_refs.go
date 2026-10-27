package cache

import (
	"cmp"
	"slices"

	"github.com/docker/docker/api/types/swarm"
)

// ServiceRef is a lightweight reference to a service. Rollback marks a service
// whose previous spec alone names the config or secret: a rollback needs it.
type ServiceRef struct {
	AtID     string `json:"@id"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Rollback bool   `json:"rollback,omitempty"`
}

// serviceRefIndex maintains reverse indexes from resource IDs (config,
// secret, network, volume) to the services that reference them, and from
// service name to ID, which swarm keeps unique. A config or secret counts its
// previous-spec references too, marked false, since a rollback restores them.
// All methods assume the caller holds the appropriate lock on Cache.mu.
type serviceRefIndex struct {
	byName    map[string]string          // service name -> serviceID
	byConfig  map[string]map[string]bool // configID -> serviceID -> in the current spec
	bySecret  map[string]map[string]bool // secretID -> serviceID -> in the current spec
	byNetwork map[string]map[string]bool // networkID -> serviceID -> true
	byVolume  map[string]map[string]bool // volumeName -> serviceID -> true
}

func newServiceRefIndex() serviceRefIndex {
	return serviceRefIndex{
		byName:    make(map[string]string),
		byConfig:  make(map[string]map[string]bool),
		bySecret:  make(map[string]map[string]bool),
		byNetwork: make(map[string]map[string]bool),
		byVolume:  make(map[string]map[string]bool),
	}
}

// add populates the reverse indexes for a service's references, refs.
func (idx *serviceRefIndex) add(s swarm.Service, refs refSet) {
	idx.byName[s.Spec.Name] = s.ID
	for id, current := range refs.configs {
		idx.byConfig[id] = setRef(idx.byConfig[id], s.ID, current)
	}
	for id, current := range refs.secrets {
		idx.bySecret[id] = setRef(idx.bySecret[id], s.ID, current)
	}
	for id := range refs.networks {
		idx.byNetwork[id] = setRef(idx.byNetwork[id], s.ID, true)
	}
	for id := range refs.volumes {
		idx.byVolume[id] = setRef(idx.byVolume[id], s.ID, true)
	}
}

// remove removes a service and its references, refs, from all reverse indexes.
func (idx *serviceRefIndex) remove(s swarm.Service, refs refSet) {
	if idx.byName[s.Spec.Name] == s.ID {
		delete(idx.byName, s.Spec.Name)
	}
	for id := range refs.configs {
		removeRef(idx.byConfig, id, s.ID)
	}
	for id := range refs.secrets {
		removeRef(idx.bySecret, id, s.ID)
	}
	for id := range refs.networks {
		removeRef(idx.byNetwork, id, s.ID)
	}
	for id := range refs.volumes {
		removeRef(idx.byVolume, id, s.ID)
	}
}

// rebuild clears and repopulates all indexes from the given service map.
func (idx *serviceRefIndex) rebuild(services map[string]swarm.Service) {
	*idx = newServiceRefIndex()
	idx.byName = make(map[string]string, len(services))
	for _, svc := range services {
		idx.add(svc, serviceRefs(svc))
	}
}

// lookup returns ServiceRef slices for services referencing the given key
// in the given index map. The services map is needed to resolve names.
func (idx *serviceRefIndex) lookup(
	index map[string]map[string]bool,
	key string,
	services map[string]swarm.Service,
) []ServiceRef {
	svcIDs := index[key]
	if len(svcIDs) == 0 {
		return nil
	}
	refs := make([]ServiceRef, 0, len(svcIDs))
	for svcID, current := range svcIDs {
		if svc, ok := services[svcID]; ok {
			refs = append(refs, ServiceRef{
				AtID:     "/services/" + svc.ID,
				ID:       svc.ID,
				Name:     svc.Spec.Name,
				Rollback: !current,
			})
		}
	}
	slices.SortFunc(refs, func(a, b ServiceRef) int {
		if byName := cmp.Compare(a.Name, b.Name); byName != 0 {
			return byName
		}

		return cmp.Compare(a.ID, b.ID)
	})
	return refs
}

func setRef(services map[string]bool, svcID string, current bool) map[string]bool {
	if services == nil {
		services = make(map[string]bool)
	}
	services[svcID] = current

	return services
}

func removeRef(idx map[string]map[string]bool, key, svcID string) {
	if m := idx[key]; m != nil {
		delete(m, svcID)
		if len(m) == 0 {
			delete(idx, key)
		}
	}
}
