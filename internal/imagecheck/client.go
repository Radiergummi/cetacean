package imagecheck

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	json "github.com/goccy/go-json"
)

const (
	maxManifestSize = 4 << 20
	maxTokenSize    = 64 << 10

	// The token specification's default for a token that states no lifetime.
	defaultTokenLifetime = 60 * time.Second
)

// manifestAccept names the index types first, so a multi-platform tag
// answers with its index digest: the one Swarm pins, not one platform's.
var manifestAccept = strings.Join([]string{
	"application/vnd.oci.image.index.v1+json",
	"application/vnd.docker.distribution.manifest.list.v2+json",
	"application/vnd.oci.image.manifest.v1+json",
	"application/vnd.docker.distribution.manifest.v2+json",
}, ", ")

var (
	// ErrUnauthorized is a registry refusing an anonymous pull: a private
	// image, which Cetacean holds no credentials for.
	ErrUnauthorized = errors.New("registry refused anonymous access")

	// ErrNotFound is a registry that does not know the tag.
	ErrNotFound = errors.New("tag not found in registry")
)

// Client resolves tags against registries over HTTPS, anonymously.
type Client struct {
	http *http.Client

	mu     sync.Mutex
	tokens map[string]bearerToken
}

type bearerToken struct {
	value   string
	expires time.Time
}

// NewClient returns a client sending its requests through httpClient.
func NewClient(httpClient *http.Client) *Client {
	return &Client{http: httpClient, tokens: map[string]bearerToken{}}
}

// Resolve returns the digest of the manifest ref's tag names now.
func (c *Client) Resolve(ctx context.Context, ref Reference) (string, error) {
	manifestURL := "https://" + ref.Host + "/v2/" + ref.Repository + "/manifests/" +
		url.PathEscape(ref.Tag)

	resp, err := c.send(ctx, http.MethodHead, manifestURL, ref)
	if err != nil {
		return "", err
	}

	_ = resp.Body.Close()

	if err := statusError(resp.StatusCode); err != nil {
		return "", err
	}

	if digest := resp.Header.Get("Docker-Content-Digest"); digestGrammar.MatchString(digest) {
		return digest, nil
	}

	// The header is only a SHOULD; without it the digest is the body's own.
	return c.digestOfBody(ctx, manifestURL, ref)
}

func (c *Client) digestOfBody(
	ctx context.Context,
	manifestURL string,
	ref Reference,
) (string, error) {
	resp, err := c.send(ctx, http.MethodGet, manifestURL, ref)
	if err != nil {
		return "", err
	}

	defer func() { _ = resp.Body.Close() }()

	if err := statusError(resp.StatusCode); err != nil {
		return "", err
	}

	stated := resp.Header.Get("Docker-Content-Digest")

	var h hash.Hash

	algorithm := "sha256"
	if strings.HasPrefix(stated, "sha512:") {
		algorithm, h = "sha512", sha512.New()
	} else {
		h = sha256.New()
	}

	n, err := io.Copy(h, io.LimitReader(resp.Body, maxManifestSize+1))
	if err != nil {
		return "", err
	}

	if n > maxManifestSize {
		return "", fmt.Errorf("manifest larger than %d bytes", maxManifestSize)
	}

	digest := algorithm + ":" + hex.EncodeToString(h.Sum(nil))

	// A digest the registry states is one we use, so it has to be the body's.
	if stated != "" && stated != digest {
		return "", fmt.Errorf(
			"registry stated digest %s for a manifest hashing to %s",
			stated,
			digest,
		)
	}

	return digest, nil
}

func statusError(code int) error {
	switch code {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUnauthorized
	default:
		return fmt.Errorf("registry answered %d", code)
	}
}

// send makes the request, answering a Bearer challenge with an anonymous
// pull token once. The caller closes the body.
func (c *Client) send(
	ctx context.Context,
	method, target string,
	ref Reference,
) (*http.Response, error) {
	cacheKey := ref.Host + "/" + ref.Repository

	resp, err := c.attempt(ctx, method, target, c.cachedToken(cacheKey))
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}

	challenges := parseChallenges(resp.Header.Values("WWW-Authenticate"))
	_ = resp.Body.Close()

	var bearer *challenge
	for i := range challenges {
		if challenges[i].scheme == "bearer" {
			bearer = &challenges[i]

			break
		}
	}

	if bearer == nil {
		return nil, ErrUnauthorized
	}

	token, err := c.fetchToken(ctx, *bearer, ref)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.tokens[cacheKey] = token
	c.mu.Unlock()

	return c.attempt(ctx, method, target, token.value)
}

func (c *Client) attempt(
	ctx context.Context,
	method, target, token string,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", manifestAccept)

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return c.http.Do(req)
}

func (c *Client) cachedToken(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	token, ok := c.tokens[key]
	if !ok || time.Now().After(token.expires) {
		delete(c.tokens, key)

		return ""
	}

	return token.value
}

// fetchToken asks the challenge's realm for an anonymous pull token. The realm
// must be HTTPS: it is the registry's to name, and a token sent in the clear
// would let anyone on the path replay it.
func (c *Client) fetchToken(ctx context.Context, ch challenge, ref Reference) (bearerToken, error) {
	realm, err := url.Parse(ch.params["realm"])
	if err != nil || realm.Scheme != "https" || realm.Host == "" {
		return bearerToken{}, errors.New("registry named an unusable token realm")
	}

	scope := ch.params["scope"]
	if scope == "" {
		scope = "repository:" + ref.Repository + ":pull"
	}

	query := realm.Query()
	query.Set("scope", scope)

	if service := ch.params["service"]; service != "" {
		query.Set("service", service)
	}

	realm.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm.String(), nil)
	if err != nil {
		return bearerToken{}, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return bearerToken{}, err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return bearerToken{}, ErrUnauthorized
	}

	if resp.StatusCode != http.StatusOK {
		return bearerToken{}, fmt.Errorf("token service answered %d", resp.StatusCode)
	}

	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxTokenSize)).Decode(&body); err != nil {
		return bearerToken{}, errors.New("token service answered an unreadable body")
	}

	value := body.Token
	if value == "" {
		value = body.AccessToken
	}

	if value == "" {
		return bearerToken{}, errors.New("token service answered no token")
	}

	lifetime := defaultTokenLifetime
	if body.ExpiresIn > 0 {
		lifetime = time.Duration(body.ExpiresIn) * time.Second
	}

	// Renewed early, so a token cannot expire between the check and its use.
	return bearerToken{value: value, expires: time.Now().Add(lifetime * 9 / 10)}, nil
}
