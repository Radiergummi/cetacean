package integrations

import (
	"cmp"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// CaddyIntegration represents the Caddyfile caddy-docker-proxy builds from a
// service's labels. Label values are Go templates, kept unevaluated.
type CaddyIntegration struct {
	Name      string   `json:"name"`
	Enabled   bool     `json:"enabled"`
	Labels    []string `json:"labels"`
	Caddyfile string   `json:"caddyfile"`
}

// The label filter and path grammar are caddy-docker-proxy's own, from
// generator/generator.go and caddyfile/fromlabels.go.
var (
	caddyLabel = regexp.MustCompile(`^caddy(_\d+)?(\.|$)`)
	caddyPath  = regexp.MustCompile(`^(?:(.+)\.)?(?:(\d+)_)?([^.]+?)(?:_(\d+))?$`)
)

const caddyEscapedDot = "\x00"

type caddyBlock struct {
	order    int
	keys     []string
	children []*caddyBlock
}

func detectCaddy(labels map[string]string) *CaddyIntegration {
	root := &caddyBlock{}
	byPath := map[string]*caddyBlock{}

	var claimed []string

	for k, v := range labels {
		if !caddyLabel.MatchString(k) {
			continue
		}

		claimed = append(claimed, k)

		block := caddyBlockAt(root, k, byPath)
		if block == nil {
			continue
		}

		if v = strings.TrimSpace(v); v != "" {
			block.keys = append(block.keys, v)
		}
	}

	if len(root.children) == 0 {
		return nil
	}

	root.sort()
	slices.Sort(claimed)

	var b strings.Builder
	for _, block := range root.children {
		block.write(&b, 0)
	}

	return &CaddyIntegration{
		Name:      "caddy",
		Enabled:   true,
		Labels:    claimed,
		Caddyfile: b.String(),
	}
}

// caddyBlockAt returns nil for a path with an empty segment, such as
// `caddy.` or `caddy..x`, which the path grammar doesn't match.
func caddyBlockAt(root *caddyBlock, path string, byPath map[string]*caddyBlock) *caddyBlock {
	if block, ok := byPath[path]; ok {
		return block
	}

	match := caddyPath.FindStringSubmatch(strings.ReplaceAll(path, `\.`, caddyEscapedDot))
	if match == nil {
		return nil
	}

	parentPath := strings.ReplaceAll(match[1], caddyEscapedDot, `\.`)
	name := strings.ReplaceAll(match[3], caddyEscapedDot, ".")

	block := &caddyBlock{order: math.MaxInt32}
	if match[2] != "" {
		block.order, _ = strconv.Atoi(match[2])
	}

	if parentPath == "" {
		root.children = append(root.children, block)
	} else {
		parent := caddyBlockAt(root, parentPath, byPath)
		if parent == nil {
			return nil
		}

		block.keys = []string{name}
		parent.children = append(parent.children, block)
	}

	byPath[path] = block

	return block
}

func (b *caddyBlock) sort() {
	for _, child := range b.children {
		child.sort()
	}

	slices.SortStableFunc(b.children, compareCaddyBlocks)
}

func (b *caddyBlock) isGlobal() bool { return len(b.keys) == 0 }

func (b *caddyBlock) isSnippet() bool {
	return len(b.keys) == 1 && strings.HasPrefix(b.keys[0], "(") &&
		strings.HasSuffix(b.keys[0], ")")
}

func (b *caddyBlock) isMatcher() bool { return len(b.keys) > 0 && strings.HasPrefix(b.keys[0], "@") }

// compareCaddyBlocks orders blocks as caddy-docker-proxy's marshaller does:
// global options, snippets, matchers, explicit order, then keys and children.
func compareCaddyBlocks(a, b *caddyBlock) int {
	for _, first := range []func(*caddyBlock) bool{(*caddyBlock).isGlobal, (*caddyBlock).isSnippet, (*caddyBlock).isMatcher} {
		if first(a) != first(b) {
			if first(a) {
				return -1
			}

			return 1
		}
	}

	if c := cmp.Compare(a.order, b.order); c != 0 {
		return c
	}

	if c := slices.Compare(a.keys, b.keys); c != 0 {
		return c
	}

	return slices.CompareFunc(a.children, b.children, compareCaddyBlocks)
}

func (b *caddyBlock) write(w *strings.Builder, level int) {
	w.WriteString(strings.Repeat("\t", level))
	w.WriteString(strings.Join(b.keys, " "))

	if len(b.children) > 0 {
		if len(b.keys) > 0 {
			w.WriteString(" ")
		}

		w.WriteString("{\n")

		for _, child := range b.children {
			child.write(w, level+1)
		}

		w.WriteString(strings.Repeat("\t", level))
		w.WriteString("}")
	}

	w.WriteString("\n")
}
