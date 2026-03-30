package cache

import "github.com/docker/docker/api/types/swarm"

// ServiceRef is a lightweight reference to a service.
type ServiceRef struct {
	AtID string `json:"@id"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// serviceRefIndex maintains reverse indexes from resource IDs (config,
// secret, network, volume) to the set of service IDs that reference them.
// All methods assume the caller holds the appropriate lock on Cache.mu.
type serviceRefIndex struct {
	byConfig  map[string]map[string]struct{} // configID -> set of serviceIDs
	bySecret  map[string]map[string]struct{} // secretID -> set of serviceIDs
	byNetwork map[string]map[string]struct{} // networkID -> set of serviceIDs
	byVolume  map[string]map[string]struct{} // volumeName -> set of serviceIDs
}

func newServiceRefIndex() serviceRefIndex {
	return serviceRefIndex{
		byConfig:  make(map[string]map[string]struct{}),
		bySecret:  make(map[string]map[string]struct{}),
		byNetwork: make(map[string]map[string]struct{}),
		byVolume:  make(map[string]map[string]struct{}),
	}
}

// add populates the reverse indexes for a service's resource references.
func (idx *serviceRefIndex) add(s swarm.Service) {
	if cs := s.Spec.TaskTemplate.ContainerSpec; cs != nil {
		for _, cfg := range cs.Configs {
			addRef(idx.byConfig, cfg.ConfigID, s.ID)
		}
		for _, sec := range cs.Secrets {
			addRef(idx.bySecret, sec.SecretID, s.ID)
		}
		for _, m := range cs.Mounts {
			if m.Type == "volume" && m.Source != "" {
				addRef(idx.byVolume, m.Source, s.ID)
			}
		}
	}
	for _, n := range s.Spec.TaskTemplate.Networks {
		addRef(idx.byNetwork, n.Target, s.ID)
	}
}

// remove removes a service from all reverse indexes.
func (idx *serviceRefIndex) remove(s swarm.Service) {
	if cs := s.Spec.TaskTemplate.ContainerSpec; cs != nil {
		for _, cfg := range cs.Configs {
			removeRef(idx.byConfig, cfg.ConfigID, s.ID)
		}
		for _, sec := range cs.Secrets {
			removeRef(idx.bySecret, sec.SecretID, s.ID)
		}
		for _, m := range cs.Mounts {
			if m.Type == "volume" && m.Source != "" {
				removeRef(idx.byVolume, m.Source, s.ID)
			}
		}
	}
	for _, n := range s.Spec.TaskTemplate.Networks {
		removeRef(idx.byNetwork, n.Target, s.ID)
	}
}

// rebuild clears and repopulates all indexes from the given service map.
func (idx *serviceRefIndex) rebuild(services map[string]swarm.Service) {
	idx.byConfig = make(map[string]map[string]struct{})
	idx.bySecret = make(map[string]map[string]struct{})
	idx.byNetwork = make(map[string]map[string]struct{})
	idx.byVolume = make(map[string]map[string]struct{})
	for _, svc := range services {
		idx.add(svc)
	}
}

// lookup returns ServiceRef slices for services referencing the given key
// in the given index map. The services map is needed to resolve names.
func (idx *serviceRefIndex) lookup(
	index map[string]map[string]struct{},
	key string,
	services map[string]swarm.Service,
) []ServiceRef {
	svcIDs := index[key]
	if len(svcIDs) == 0 {
		return nil
	}
	refs := make([]ServiceRef, 0, len(svcIDs))
	for svcID := range svcIDs {
		if svc, ok := services[svcID]; ok {
			refs = append(refs, ServiceRef{
				AtID: "/services/" + svc.ID,
				ID:   svc.ID,
				Name: svc.Spec.Name,
			})
		}
	}
	return refs
}

func addRef(idx map[string]map[string]struct{}, key, svcID string) {
	if idx[key] == nil {
		idx[key] = make(map[string]struct{})
	}
	idx[key][svcID] = struct{}{}
}

func removeRef(idx map[string]map[string]struct{}, key, svcID string) {
	if m := idx[key]; m != nil {
		delete(m, svcID)
		if len(m) == 0 {
			delete(idx, key)
		}
	}
}
