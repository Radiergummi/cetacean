package attestations

import (
	"cmp"
	"slices"
	"strings"
)

// JoinDockerfile places an image's layers, in manifest order, on the lines of
// the Dockerfile its mode=max provenance embeds. A layer belongs to the step
// whose layer chain ends in it, which is the step that created it; a layer no
// step created is listed as unattributed.
func JoinDockerfile(statement []byte, layers []Descriptor) (*Dockerfile, error) {
	p, err := parseStatement(statement)
	if err != nil {
		return nil, err
	}

	bk := p.buildkit
	if bk == nil || bk.Source.dockerfileIndex() < 0 {
		return nil, ErrNoSource
	}

	source := bk.Source
	index := source.dockerfileIndex()

	d := &Dockerfile{
		Path:  p.provenance.DockerfilePath,
		Lines: strings.Split(strings.TrimSuffix(string(source.Infos[index].Data), "\n"), "\n"),
	}

	base := baseSteps(p.buildConfig)
	steps := map[string]*DockerfileStep{}

	for id, loc := range source.Locations {
		step := &DockerfileStep{Base: base[id]}

		for _, l := range loc.Locations {
			if l.SourceIndex != index {
				continue
			}

			for _, r := range l.Ranges {
				step.Ranges = append(step.Ranges, [2]int{r.Start.Line, r.End.Line})
			}
		}

		if len(step.Ranges) > 0 {
			slices.SortFunc(step.Ranges, func(a, b [2]int) int { return cmp.Compare(a[0], b[0]) })
			steps[id] = step
		}
	}

	creator := creators(bk.Layers)

	for _, layer := range layers {
		d.TotalSize += layer.Size

		step, ok := steps[creator[layer.Digest]]
		if !ok {
			d.Unattributed = append(d.Unattributed, layer)

			continue
		}

		step.Layers = append(step.Layers, layer)
		if step.Base {
			d.BaseSize += layer.Size
		}
	}

	for _, step := range steps {
		d.Steps = append(d.Steps, *step)
	}

	slices.SortFunc(d.Steps, func(a, b DockerfileStep) int {
		return cmp.Compare(a.Ranges[0][0], b.Ranges[0][0])
	})

	return d, nil
}

// creators maps each layer to the step whose chain ends in it. Keys are
// <step>:<output>; the sorted walk keeps the answer stable where two match.
func creators(layers map[string][][]Descriptor) map[string]string {
	keys := make([]string, 0, len(layers))
	for k := range layers {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	out := map[string]string{}

	for _, key := range keys {
		step, _, _ := strings.Cut(key, ":")

		for _, chain := range layers[key] {
			if len(chain) == 0 {
				continue
			}

			if last := chain[len(chain)-1].Digest; out[last] == "" {
				out[last] = step
			}
		}
	}

	return out
}

// baseSteps are the LLB source operations, which is what a FROM becomes.
func baseSteps(config *buildConfig) map[string]bool {
	out := map[string]bool{}
	if config == nil {
		return out
	}

	for _, op := range config.LLBDefinition {
		if _, ok := op.Op.Op["source"]; ok {
			out[op.ID] = true
		}
	}

	return out
}
