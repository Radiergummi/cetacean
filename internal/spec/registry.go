// Package spec holds the requirement registry: what the specifications
// Cetacean implements actually require, which tests claim each requirement,
// and which edits those tests must refuse. See
// docs/specs/2026-09-16-spec-requirement-registry-design.md.
package spec

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed all:registry
var registryFS embed.FS

// Level is the RFC 2119 strength of a requirement.
type Level string

const (
	MUST   Level = "MUST"
	SHOULD Level = "SHOULD"
	MAY    Level = "MAY"
)

// URLs is one or more citations. A requirement often has two homes — the
// specification page and a machine-readable copy — and recording both is what
// makes a divergence between them visible.
type URLs []string

func (u *URLs) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		var one string
		if err := value.Decode(&one); err != nil {
			return err
		}

		*u = URLs{one}

		return nil
	case yaml.SequenceNode:
		var many []string
		if err := value.Decode(&many); err != nil {
			return err
		}

		*u = many

		return nil
	default:
		return fmt.Errorf("url must be a string or a list of strings, got %v", value.Kind)
	}
}

// Lane is a suite a requirement's evidence lives in. The empty lane is the
// unit suite, which every run includes; LaneE2E is the build-tagged suite,
// which needs a Docker environment and which CI does not run.
type Lane string

const LaneE2E Lane = "e2e"

// Mutant is an edit that a requirement's claiming tests must refuse.
type Mutant struct {
	File    string `yaml:"file"`
	Replace string `yaml:"replace"`
	With    string `yaml:"with"`
}

type Requirement struct {
	ID    string `yaml:"id"`
	Level Level  `yaml:"level"`
	Text  string `yaml:"text"`
	URL   URLs   `yaml:"url"`

	// Deferred: knowingly unmet, and a claiming test pins the current answer.
	// Gap: implemented and transcribed, no test yet. Exactly one may be set.
	Deferred string `yaml:"deferred"`
	Gap      string `yaml:"gap"`

	// Lane names the suite that reaches this requirement when the unit one
	// cannot. It is what the report counts as not run rather than uncovered,
	// so the static gate holds it to the claimants that exist.
	Lane Lane `yaml:"lane"`

	Mutants []Mutant `yaml:"mutants"`

	Document *Document `yaml:"-"`
}

func (q *Requirement) FullID() string {
	return q.Document.Key() + "/" + q.ID
}

// Inventory names where the document's complete requirement set comes from.
// Requirements plus Dismissed must account for all of it, which is what stops
// a requirement being deleted to make the gate green.
type Inventory struct {
	From  string `yaml:"from"`
	Count int    `yaml:"count"`
}

type Document struct {
	Source       string            `yaml:"source"`
	Title        string            `yaml:"title"`
	Revision     string            `yaml:"revision"`
	Reviewed     string            `yaml:"reviewed"`
	URL          URLs              `yaml:"url"`
	Inventory    *Inventory        `yaml:"inventory"`
	Requirements []Requirement     `yaml:"requirements"`
	Dismissed    map[string]string `yaml:"dismissed"`

	Family string `yaml:"-"`
	Name   string `yaml:"-"`
}

func (d *Document) Key() string { return d.Family + "/" + d.Name }

type Registry struct {
	Documents []*Document

	byID map[string]*Requirement
}

func (r *Registry) Lookup(id string) (*Requirement, bool) {
	q, ok := r.byID[id]

	return q, ok
}

func (r *Registry) All() []*Requirement {
	out := make([]*Requirement, 0, len(r.byID))
	for _, d := range r.Documents {
		for i := range d.Requirements {
			out = append(out, &d.Requirements[i])
		}
	}

	return out
}

var loaded = sync.OnceValues(load)

// Load parses the embedded registry once per process.
func Load() (*Registry, error) { return loaded() }

// familyAndName parses a walked path like "registry/oauth/rfc7636.yaml",
// returning the family and document name. It errors if the family is empty
// or contains slashes, naming the offending path.
func familyAndName(p string) (family, name string, err error) {
	rel := strings.TrimPrefix(p, "registry/")

	family, file := path.Split(rel)
	family = strings.TrimSuffix(family, "/")

	if family == "" {
		return "", "", fmt.Errorf("spec: %s must live in a family directory", p)
	}

	if strings.Contains(family, "/") {
		return "", "", fmt.Errorf("spec: %s: family segment cannot contain /", p)
	}

	return family, strings.TrimSuffix(file, ".yaml"), nil
}

func load() (*Registry, error) {
	reg := &Registry{byID: map[string]*Requirement{}}

	err := fs.WalkDir(registryFS, "registry", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".yaml") {
			return err
		}

		rel := strings.TrimPrefix(p, "registry/")
		if rel == "unregistered.yaml" {
			return nil
		}

		family, name, err := familyAndName(p)
		if err != nil {
			return err
		}

		body, err := registryFS.ReadFile(p)
		if err != nil {
			return err
		}

		doc := &Document{Family: family, Name: name}
		if err := yaml.Unmarshal(body, doc); err != nil {
			return fmt.Errorf("spec: %s: %w", p, err)
		}

		if err := reg.add(doc); err != nil {
			return fmt.Errorf("spec: %s: %w", p, err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return reg, nil
}

// NewForTest builds a registry in memory. scripts/spec-gate is a different
// package and cannot reach the unexported constructor, so its tests need this
// even though nothing shipped calls it.
func NewForTest(family, name string, reqs []Requirement) (*Registry, error) {
	reg := &Registry{byID: map[string]*Requirement{}}
	doc := &Document{Family: family, Name: name, Requirements: reqs}

	if err := reg.add(doc); err != nil {
		return nil, err
	}

	return reg, nil
}

func (r *Registry) add(doc *Document) error {
	for i := range doc.Requirements {
		q := &doc.Requirements[i]
		q.Document = doc

		if err := q.validate(); err != nil {
			return err
		}

		if _, dup := r.byID[q.FullID()]; dup {
			return fmt.Errorf("duplicate requirement %q", q.FullID())
		}

		r.byID[q.FullID()] = q
	}

	r.Documents = append(r.Documents, doc)

	return nil
}

// validate rejects what the claims line format and the gates cannot express.
func (q *Requirement) validate() error {
	if q.ID == "" {
		return fmt.Errorf("requirement with no id in %s", q.Document.Key())
	}

	if strings.ContainsAny(q.ID, "\t\n") {
		return fmt.Errorf("id %q contains a tab or newline", q.ID)
	}

	switch q.Level {
	case MUST, SHOULD, MAY:
	default:
		return fmt.Errorf("%s: level = %q, want MUST, SHOULD or MAY", q.FullID(), q.Level)
	}

	if strings.TrimSpace(q.Text) == "" {
		return fmt.Errorf("%s: empty text", q.FullID())
	}

	if q.Deferred != "" && q.Gap != "" {
		return fmt.Errorf("%s: both deferred and gap are set", q.FullID())
	}

	if q.Lane != "" && q.Lane != LaneE2E {
		return fmt.Errorf("%s: lane = %q, want e2e or nothing", q.FullID(), q.Lane)
	}

	return nil
}

// Token canonicalises a specification's name to the one spelling the sweep
// compares on: "RFC 7636", "rfc7636" and "RFC-7636" all become "RFC7636";
// "SEP 2575" becomes "SEP-2575". A registered document and a citation of it
// have to land on the same string, so both sides call this. The dot goes too,
// so a document named by version — "OAuth 2.1", filed as oauth-2-1.yaml —
// meets its citations.
func Token(name string) string {
	upper := strings.ToUpper(strings.NewReplacer(" ", "", "-", "", ".", "").Replace(name))

	if num, ok := strings.CutPrefix(upper, "SEP"); ok {
		return "SEP-" + num
	}

	return upper
}

// Token is how this document is named in a comment.
func (d *Document) Token() string { return Token(d.Name) }

// Unregistered lists specifications the tree cites and this registry
// deliberately says nothing about, each with its reason.
func Unregistered() (map[string]string, error) {
	body, err := registryFS.ReadFile("registry/unregistered.yaml")
	if err != nil {
		return nil, err
	}

	out := map[string]string{}
	if err := yaml.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("spec: registry/unregistered.yaml: %w", err)
	}

	return out, nil
}

// Validate reports the document-level rules: the inventory denominator, and
// that every dismissal carries a reason.
func (r *Registry) Validate() []error {
	var errs []error

	for _, doc := range r.Documents {
		for id, reason := range doc.Dismissed {
			if strings.TrimSpace(reason) == "" {
				errs = append(errs, fmt.Errorf("%s: dismissal %q has no reason", doc.Key(), id))
			}
		}

		if doc.Inventory == nil {
			continue
		}

		accounted := len(doc.Requirements) + len(doc.Dismissed)
		if accounted != doc.Inventory.Count {
			errs = append(errs, fmt.Errorf(
				"%s: %d requirements + %d dismissed = %d, but the inventory declares %d",
				doc.Key(), len(doc.Requirements), len(doc.Dismissed),
				accounted, doc.Inventory.Count,
			))
		}
	}

	return errs
}
