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
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync"
	"time"

	json "github.com/goccy/go-json"
)

// MaxManifestSize is the most of a manifest the client reads.
const MaxManifestSize = 4 << 20

const (
	maxTokenSize = 64 << 10

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

	// ErrNotFound is a registry that does not know the tag or digest.
	ErrNotFound = errors.New("not found in registry")

	// ErrTooLarge is content over the size its caller allowed.
	ErrTooLarge = errors.New("registry content larger than allowed")

	errForeignRealm = errors.New("registry named a token realm on another site")
)

// Client resolves tags against registries over HTTPS, anonymously.
type Client struct {
	http  *http.Client
	proxy func(*http.Request) (*url.URL, error)

	mu     sync.Mutex
	tokens map[string]bearerToken
}

type bearerToken struct {
	value   string
	expires time.Time
}

// NewClient returns a client sending its requests through a copy of
// httpClient that follows only redirects redirectPolicy admits. net/http keeps
// Authorization across a redirect to the same host on another port; a pull
// token is for the registry alone, so the copy drops it.
func NewClient(httpClient *http.Client) *Client {
	guarded := *httpClient

	transport := guarded.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	proxy := proxyOf(transport)
	policy := redirectPolicy(proxy)
	guarded.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := policy(req, via); err != nil {
			return err
		}

		if req.URL.Host != via[0].URL.Host {
			req.Header.Del("Authorization")
		}

		return nil
	}

	return &Client{http: &guarded, proxy: proxy, tokens: map[string]bearerToken{}}
}

// Manifest fetches the manifest digest names, refusing a body that does not
// hash to it, and returns it with the media type the registry states.
func (c *Client) Manifest(
	ctx context.Context,
	ref Reference,
	digest string,
) ([]byte, string, error) {
	target := "https://" + ref.Host + "/v2/" + ref.Repository + "/manifests/" + digest

	body, header, err := c.fetchDigested(ctx, target, ref, digest, MaxManifestSize)
	if err != nil {
		return nil, "", err
	}

	return body, header.Get("Content-Type"), nil
}

// Blob fetches the blob digest names, at most limit bytes of it, refusing a
// body that does not hash to it.
func (c *Client) Blob(
	ctx context.Context,
	ref Reference,
	digest string,
	limit int64,
) ([]byte, error) {
	target := "https://" + ref.Host + "/v2/" + ref.Repository + "/blobs/" + digest
	body, _, err := c.fetchDigested(ctx, target, ref, digest, limit)

	return body, err
}

func (c *Client) fetchDigested(
	ctx context.Context,
	target string,
	ref Reference,
	digest string,
	limit int64,
) ([]byte, http.Header, error) {
	// The digest becomes part of the URL, so it must be one and nothing more.
	if !ValidDigest(digest) {
		return nil, nil, fmt.Errorf("not a digest: %q", digest)
	}

	resp, err := c.send(c.forRegistry(ctx, ref, target), http.MethodGet, target, ref)
	if err != nil {
		return nil, nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	if err := statusError(resp.StatusCode); err != nil {
		return nil, nil, err
	}

	if resp.ContentLength > limit {
		return nil, nil, ErrTooLarge
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, nil, err
	}

	if int64(len(body)) > limit {
		return nil, nil, ErrTooLarge
	}

	algorithm, h := hasherFor(digest)
	h.Write(body)

	if got := algorithm + ":" + hex.EncodeToString(h.Sum(nil)); got != digest {
		return nil, nil, fmt.Errorf("requested %s, but the registry sent content hashing to %s",
			digest, got)
	}

	return body, resp.Header, nil
}

// hasherFor is the hash a digest names, sha256 for anything but sha512.
func hasherFor(digest string) (string, hash.Hash) {
	if strings.HasPrefix(digest, "sha512:") {
		return "sha512", sha512.New()
	}

	return "sha256", sha256.New()
}

// forRegistry records ref's registry in ctx for screen and the realm rule.
// Through a proxy the connection reaches the proxy, which says nothing about
// where the registry is, so it stays off the LAN.
func (c *Client) forRegistry(ctx context.Context, ref Reference, target string) context.Context {
	ctx = withRegistry(ctx, ref.Host)
	if !proxied(c.proxy, target) {
		registry := registryOf(ctx)
		ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
			GotConn: func(info httptrace.GotConnInfo) { registry.connected(info.Conn.RemoteAddr()) },
		})
	}

	return ctx
}

// Resolve returns the digest of the manifest ref's tag names now.
func (c *Client) Resolve(ctx context.Context, ref Reference) (string, error) {
	manifestURL := "https://" + ref.Host + "/v2/" + ref.Repository + "/manifests/" +
		url.PathEscape(ref.Tag)

	resp, err := c.send(c.forRegistry(ctx, ref, manifestURL), http.MethodHead, manifestURL, ref)
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
	algorithm, h := hasherFor(stated)

	n, err := io.Copy(h, io.LimitReader(resp.Body, MaxManifestSize+1))
	if err != nil {
		return "", err
	}

	if n > MaxManifestSize {
		return "", fmt.Errorf("manifest larger than %d bytes", MaxManifestSize)
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

	token, err := c.fetchToken(ctx, *bearer, ref, resp.Request.URL.Host)
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
// must be HTTPS, or anyone on the path could replay the token, and one
// realmAllowed admits, or any registry could aim the request anywhere.
func (c *Client) fetchToken(
	ctx context.Context,
	ch challenge,
	ref Reference,
	challenger string,
) (bearerToken, error) {
	realm, err := url.Parse(ch.params["realm"])
	if err != nil || realm.Scheme != "https" || realm.Host == "" {
		return bearerToken{}, errors.New("registry named an unusable token realm")
	}

	if !realmAllowed(ctx, realm.Host, challenger) {
		return bearerToken{}, fmt.Errorf("%w: %s", errForeignRealm, realm.Host)
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
