package api

import (
	"slices"
	"sync"
)

// projectionCacheSize bounds how many (generation, audience) pairs are kept:
// one serves a deployment without ACL policy, a handful one with a few roles.
// Past that it evicts, and the endpoint costs what it would uncached.
const projectionCacheSize = 16

// projectionKey identifies a rendered document. The fingerprint is
// load-bearing: these documents are ACL-filtered, and two identities must never
// be handed each other's. scope names the resource, or is empty for
// cluster-wide documents.
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
// Entries are immutable once stored and a hit shares the backing array, so
// nothing may write to a returned document.
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

// put stores doc under key, dropping superseded generations first — they can
// never be served again, and waiting for FIFO keeps large dead documents
// resident — then evicting the oldest entry if the cache is still full.
func (p *projectionCache) put(key projectionKey, doc renderedDoc) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.entries[key]; exists {
		return
	}

	p.order = slices.DeleteFunc(p.order, func(k projectionKey) bool {
		if k.generation >= key.generation {
			return false
		}
		delete(p.entries, k)

		return true
	})

	if len(p.order) >= projectionCacheSize {
		delete(p.entries, p.order[0])
		p.order = slices.Delete(p.order, 0, 1)
	}

	p.entries[key] = doc
	p.order = append(p.order, key)
}
