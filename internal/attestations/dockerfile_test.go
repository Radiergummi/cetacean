package attestations

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	json "github.com/goccy/go-json"
)

func joinedProbe(t *testing.T, layers []Descriptor) *Dockerfile {
	t.Helper()

	d, err := JoinDockerfile(probeBlob(t, probeProvenance), layers)
	if err != nil {
		t.Fatalf("JoinDockerfile: %v", err)
	}

	return d
}

// stepAt returns the step one of whose ranges starts on line.
func stepAt(d *Dockerfile, line int) *DockerfileStep {
	for i, s := range d.Steps {
		for _, r := range s.Ranges {
			if r[0] == line {
				return &d.Steps[i]
			}
		}
	}

	return nil
}

func TestJoinDockerfileProbe(t *testing.T) {
	layers := probeLayers(t)
	d := joinedProbe(t, layers)

	if d.Path != "Dockerfile" || d.Lines[3] != "FROM alpine:3.20" {
		t.Errorf("path %q, line 4 %q", d.Path, d.Lines[3])
	}

	sizes := strings.Fields(probeLayerSizes)
	for i, line := range []int{4, 5, 6, 7, 8, 9} {
		step := stepAt(d, line)
		if step == nil || len(step.Layers) != 1 {
			t.Errorf("line %d: step %+v, want one layer", line, step)

			continue
		}

		if got := strconv.FormatInt(step.Layers[0].Size, 10); got != sizes[i] {
			t.Errorf("line %d: layer of %s bytes, want %s", line, got, sizes[i])
		}
	}

	from := stepAt(d, 4)
	if from == nil || !from.Base || len(from.Ranges) != 2 {
		t.Errorf("FROM step = %+v; want a base step on lines 1 and 4", from)
	}

	var total int64
	for _, l := range layers {
		total += l.Size
	}

	if d.TotalSize != total || d.BaseSize != 4092319 || len(d.Unattributed) != 0 {
		t.Errorf("total %d (want %d), base %d, unattributed %v",
			d.TotalSize, total, d.BaseSize, d.Unattributed)
	}

	if len(layers) != probeTotalLayers {
		t.Fatalf("probe has %d layers", len(layers))
	}
}

func TestJoinDockerfileUnclaimedLayer(t *testing.T) {
	stray := Descriptor{Digest: "sha256:" + strings.Repeat("f", 64), Size: 7}
	layers := append(probeLayers(t), stray)

	d := joinedProbe(t, layers)
	if len(d.Unattributed) != 1 || d.Unattributed[0] != stray {
		t.Errorf("unattributed = %v", d.Unattributed)
	}

	var total int64
	for _, l := range layers {
		total += l.Size
	}

	if d.TotalSize != total {
		t.Errorf("total = %d, want %d", d.TotalSize, total)
	}
}

func TestJoinDockerfileNoSource(t *testing.T) {
	var statement map[string]any
	if err := json.Unmarshal(fixture(t, "syft-provenance.json"), &statement); err != nil {
		t.Fatal(err)
	}

	metadata := statement["predicate"].(map[string]any)["runDetails"].(map[string]any)["metadata"].(map[string]any)
	delete(metadata["buildkit_metadata"].(map[string]any), "source")

	body, _ := json.Marshal(statement)
	if _, err := JoinDockerfile(body, nil); !errors.Is(err, ErrNoSource) {
		t.Errorf("err = %v, want ErrNoSource", err)
	}
}

// docker-library swaps nginx's FROM image in through a build context, so its
// source step has no line; its layer is still the base image's.
func TestJoinDockerfileCountsABaseWithoutALine(t *testing.T) {
	base := Descriptor{
		Digest: "sha256:f2be67225471500c788ce88c597eb5ea8336e912ad1ca4815650e0281afe2034",
		Size:   29843579,
	}

	d, err := JoinDockerfile(fixture(t, "nginx-provenance.json"), []Descriptor{base})
	if err != nil {
		t.Fatal(err)
	}

	if d.BaseSize != base.Size {
		t.Errorf("base = %d, want %d", d.BaseSize, base.Size)
	}

	if len(d.Unattributed) != 1 || d.Unattributed[0] != base {
		t.Errorf("unattributed = %v; a layer no line produced is still listed", d.Unattributed)
	}
}

func TestJoinDockerfileV02(t *testing.T) {
	d, err := JoinDockerfile(fixture(t, "nginx-provenance.json"), nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(d.Lines) == 0 ||
		!strings.HasPrefix(d.Lines[0], "#") && !strings.Contains(d.Lines[0], "FROM") {
		t.Errorf("first line %q", d.Lines[0])
	}

	if len(d.Steps) == 0 {
		t.Error("no steps located in nginx's Dockerfile")
	}
}
