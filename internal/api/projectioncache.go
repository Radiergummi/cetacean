package api

import "sync"

// projectionCacheSize bounds how many (generation, audience) pairs are kept.
// One entry serves a deployment with no ACL policy; a handful serve one with a
// few roles. Past that it evicts and the endpoint costs what it always did,
// which is the right floor — a deployment with hundreds of distinct grant sets
// gets no benefit and no regression.
const projectionCacheSize = 16

// projectionKey identifies a rendered document. The generation and fingerprint
// are both load-bearing: the generation because the cluster changes underneath,
// and the fingerprint because these documents are ACL-filtered and two
// identities must never be handed each other's. scope names the resource for
// documents that have one, and is empty for cluster-wide ones.
type projectionKey struct {
	generation  uint64
	fingerprint uint64
	scope       string
}

// renderedDoc is a document and the validator for it. The validator is kept
// rather than recomputed because it is a hash of the body — on a large document
// that is most of what answering a repeat request would still cost.
type renderedDoc struct {
	body []byte
	etag string
}

// projectionCache memoises documents that are a pure function of cache contents
// and the caller's grants — nothing parameterised by a query belongs here.
//
// Entries are immutable once stored, so a hit hands back the same backing array
// to every caller. Nothing may write to a returned document.
type projectionCache struct {
	mu      sync.Mutex
	entries map[projectionKey]renderedDoc
	// order records insertion sequence for eviction. A map this small does not
	// justify a real LRU, and the useful entry is almost always the newest
	// generation anyway.
	order []projectionKey
}

func newProjectionCache() *projectionCache {
	return &projectionCache{entries: make(map[projectionKey]renderedDoc, projectionCacheSize)}
}

// get returns the stored document for key, if any.
func (p *projectionCache) get(key projectionKey) (renderedDoc, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	doc, ok := p.entries[key]

	return doc, ok
}

// put stores doc under key, evicting the oldest entry if the cache is full.
func (p *projectionCache) put(key projectionKey, doc renderedDoc) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.entries[key]; exists {
		return
	}

	if len(p.order) >= projectionCacheSize {
		delete(p.entries, p.order[0])
		p.order = p.order[1:]
	}

	p.entries[key] = doc
	p.order = append(p.order, key)
}
