// Package imagecheck asks image registries which manifest a tag names now, so
// a service still running the digest its tag resolved to at deploy time can be
// told apart from one running what the tag names today.
package imagecheck

import (
	"regexp"
	"strings"
)

const (
	dockerHubDomain = "docker.io"
	dockerHubHost   = "registry-1.docker.io"
)

// The OCI distribution spec's grammars for a repository name and a tag. A
// reference outside them cannot be requested, so it is never checked.
var (
	nameGrammar = regexp.MustCompile(
		`^[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*(/[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*)*$`,
	)
	tagGrammar    = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)
	digestGrammar = regexp.MustCompile(`^(sha256:[a-f0-9]{64}|sha512:[a-f0-9]{128})$`)
)

// Reference is a tagged image as a registry is asked about it: the host to
// dial, the repository on it, and the tag.
type Reference struct {
	Host       string
	Repository string
	Tag        string
}

// Key names the reference once however a service spelled it, so `nginx:1`
// and `docker.io/library/nginx:1` share one check.
func (r Reference) Key() string {
	return r.Host + "/" + r.Repository + ":" + r.Tag
}

// ParsePinned parses a service image of the form `name:tag@digest`, the form
// Swarm stores once it has resolved the tag. A reference with no tag, or with
// no digest to compare against, has nothing to check and reports false.
func ParsePinned(image string) (ref Reference, pinned string, ok bool) {
	name, pinned, found := strings.Cut(image, "@")
	if !found || !digestGrammar.MatchString(pinned) {
		return Reference{}, "", false
	}

	colon := strings.LastIndex(name, ":")
	if colon <= strings.LastIndex(name, "/") {
		return Reference{}, "", false
	}

	host, repository := splitHost(name[:colon])
	tag := name[colon+1:]

	if !nameGrammar.MatchString(repository) || !tagGrammar.MatchString(tag) {
		return Reference{}, "", false
	}

	return Reference{Host: host, Repository: repository, Tag: tag}, pinned, true
}

// ParseDigested parses an image addressed by digest, with or without a tag:
// `name@digest` is pinned as exactly as `name:tag@digest`. The tag, when there
// is one, is kept but nothing here asks about it.
func ParseDigested(image string) (ref Reference, digest string, ok bool) {
	name, digest, found := strings.Cut(image, "@")
	if !found || !digestGrammar.MatchString(digest) {
		return Reference{}, "", false
	}

	tag := ""
	if colon := strings.LastIndex(name, ":"); colon > strings.LastIndex(name, "/") {
		name, tag = name[:colon], name[colon+1:]

		if !tagGrammar.MatchString(tag) {
			return Reference{}, "", false
		}
	}

	host, repository := splitHost(name)
	if !nameGrammar.MatchString(repository) {
		return Reference{}, "", false
	}

	return Reference{Host: host, Repository: repository, Tag: tag}, digest, true
}

// splitHost applies Docker's rule: the first component is a registry only if
// it has a dot or a port, is localhost, or has an upper-case letter. Anything
// else is a Docker Hub repository, under library/ when it has no namespace.
func splitHost(name string) (host, repository string) {
	first, rest, found := strings.Cut(name, "/")

	isHost := found && (strings.ContainsAny(first, ".:") || first == "localhost" ||
		strings.ToLower(first) != first)
	if !isHost {
		first, rest = dockerHubDomain, name
	}

	if first == "index.docker.io" {
		first = dockerHubDomain
	}

	if first != dockerHubDomain {
		return first, rest
	}

	if !strings.Contains(rest, "/") {
		rest = "library/" + rest
	}

	return dockerHubHost, rest
}
