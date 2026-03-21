package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	json "github.com/goccy/go-json"
)

const (
	dockerReleasesURL     = "https://api.github.com/repos/moby/moby/releases/latest"
	versionCacheTTL       = 1 * time.Hour
	versionErrorRetryTime = 5 * time.Minute
	githubTimeout         = 10 * time.Second
	maxResponseSize       = 256 * 1024 // 256KB
)

type DockerLatestVersion struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

type dockerVersionCache struct {
	mu        sync.RWMutex
	version   *DockerLatestVersion
	fetchedAt time.Time
	fetching  bool
	client    *http.Client
}

func newDockerVersionCache() *dockerVersionCache {
	return &dockerVersionCache{
		client: &http.Client{Timeout: githubTimeout},
	}
}

func (c *dockerVersionCache) get(ctx context.Context) (*DockerLatestVersion, error) {
	c.mu.RLock()
	if c.version != nil && time.Since(c.fetchedAt) < versionCacheTTL {
		v := c.version
		c.mu.RUnlock()
		return v, nil
	}
	fetching := c.fetching
	stale := c.version
	c.mu.RUnlock()

	// Another goroutine is already fetching; return stale value.
	if fetching {
		return stale, nil
	}

	c.mu.Lock()
	// Re-check under write lock.
	if time.Since(c.fetchedAt) < versionCacheTTL {
		v := c.version
		c.mu.Unlock()
		return v, nil
	}
	if c.fetching {
		v := c.version
		c.mu.Unlock()
		return v, nil
	}
	c.fetching = true
	c.mu.Unlock()

	v, err := c.fetch(ctx)

	c.mu.Lock()
	c.fetching = false
	if err == nil && v != nil {
		c.version = v
		c.fetchedAt = time.Now()
	} else {
		// Use a shorter retry window so we don't hammer GitHub
		// on every request during an outage, but also don't cache
		// a failure for the full hour.
		c.fetchedAt = time.Now().Add(-(versionCacheTTL - versionErrorRetryTime))
	}
	stale = c.version
	c.mu.Unlock()

	return stale, err
}

func (c *dockerVersionCache) fetch(ctx context.Context) (*DockerLatestVersion, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dockerReleasesURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, err
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}

	if err := json.Unmarshal(body, &release); err != nil {
		return nil, err
	}

	return &DockerLatestVersion{
		Version: strings.TrimPrefix(strings.TrimPrefix(release.TagName, "docker-"), "v"),
		URL:     release.HTMLURL,
	}, nil
}

var dockerVersionCacheInstance = newDockerVersionCache()

func HandleDockerLatestVersion(w http.ResponseWriter, r *http.Request) {
	v, err := dockerVersionCacheInstance.get(r.Context())
	if err != nil {
		slog.Warn("failed to fetch latest Docker version", "error", err)
	}

	if v == nil {
		writeProblem(
			w,
			r,
			http.StatusServiceUnavailable,
			"Could not determine latest Docker version",
		)
		return
	}

	writeJSONWithETag(w, r, v)
}
