package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// pageRef names a published page: where its source is read at the pinned
// revision, and the address a citation of it uses.
type pageRef struct {
	key string
	raw string
	web string

	// live: web serves the page as HTML whose heading ids can be read back,
	// so every anchor cited into it is checked against the site itself.
	live bool
}

type corpus struct {
	pages map[string]*page
	ids   map[string]map[string]bool
}

func (c *corpus) load(refs []pageRef) error {
	for _, ref := range refs {
		if _, ok := c.pages[ref.key]; ok {
			continue
		}

		body, err := fetch(ref.raw)
		if err != nil {
			return err
		}

		c.pages[ref.key] = parsePage(ref, string(body))
	}

	return nil
}

var idRE = regexp.MustCompile(`\bid="([^"]+)"`)

// cite resolves "page#anchor" to its address, refusing an anchor the page
// does not have: a citation into a heading that moved is a broken quotation.
func (c *corpus) cite(at string) (*section, string, error) {
	key, anchor, _ := strings.Cut(at, "#")

	p, ok := c.pages[key]
	if !ok {
		return nil, "", fmt.Errorf("%s: no such page loaded", at)
	}

	s, ok := p.section(anchor)
	if !ok {
		return nil, "", fmt.Errorf("%s: %s has no such heading", at, p.ref.web)
	}

	if anchor == "" {
		return s, p.ref.web, nil
	}

	if p.ref.live {
		ids, ok := c.ids[key]
		if !ok {
			body, err := fetch(p.ref.web)
			if err != nil {
				return nil, "", err
			}

			ids = map[string]bool{}
			for _, m := range idRE.FindAllStringSubmatch(string(body), -1) {
				ids[m[1]] = true
			}

			c.ids[key] = ids
		}

		if !ids[anchor] {
			return nil, "", fmt.Errorf("%s: %s serves no #%s", at, p.ref.web, anchor)
		}
	}

	return s, p.ref.web + "#" + anchor, nil
}

type hit struct {
	at       string
	sentence int
}

// locate finds the published sentences a suite's requirement quotes. Every
// sentence of it must fall inside a published one, and all of them in the one
// section; the quotation is then the published sentences, not the suite's.
func (c *corpus) locate(keys []string, within, text string) (string, string, error) {
	var common []string

	wanted := sentenceKeys(text)
	if len(wanted) == 0 {
		return "", "", fmt.Errorf("nothing to locate in %q", text)
	}

	hits := map[string][]hit{}

	for i, want := range wanted {
		var here []string

		for _, key := range keys {
			for _, s := range c.pages[key].sections {
				at := key + "#" + s.anchor
				if within != "" && at != within {
					continue
				}

				for j, published := range s.sentences {
					if strings.Contains(published, want) {
						here = append(here, at)
						hits[at] = append(hits[at], hit{at, j})

						break
					}
				}
			}
		}

		if i == 0 {
			common = here
		} else {
			common = slices.DeleteFunc(
				common,
				func(at string) bool { return !slices.Contains(here, at) },
			)
		}
	}

	switch len(common) {
	case 0:
		return "", "", fmt.Errorf("not found in the published revision")
	case 1:
	default:
		return "", "", fmt.Errorf("ambiguous: found in %s", strings.Join(common, ", "))
	}

	s, url, err := c.cite(common[0])
	if err != nil {
		return "", "", err
	}

	var picked []int
	for _, h := range hits[common[0]] {
		picked = append(picked, h.sentence)
	}

	slices.Sort(picked)

	var quote []string
	for _, i := range slices.Compact(picked) {
		quote = append(quote, s.sentences[i])
	}

	return strings.Join(quote, " "), url, nil
}

// sentenceKeys is the text a suite sentence must be found by: normalized, its
// editorial insertions dropped, and without the terminator a published
// sentence may spell differently.
func sentenceKeys(text string) []string {
	var out []string

	for _, s := range sentences(normalize(text, true)) {
		if s = strings.TrimRight(s, ".:;, "); s != "" {
			out = append(out, s)
		}
	}

	return out
}
