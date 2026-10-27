package cluster

import (
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/cache"
)

func TestVersionedNamesTheKindsWhoseUpdatesTakeAVersion(t *testing.T) {
	for kind, want := range map[string]bool{
		"service": true,
		"node":    true,
		"config":  true,
		"secret":  true,
		"task":    false,
		"stack":   false,
		"network": false,
		"volume":  false,
		"plugin":  false,
		"":        false,
	} {
		if got := Versioned(kind); got != want {
			t.Errorf("Versioned(%q) = %v, want %v", kind, got, want)
		}
	}
}

// A conditional write is only usable if a read reports the version to send
// back, so a digest carries one exactly when its type is Versioned.
func TestDigestsReportTheVersionAConditionalWriteTakes(t *testing.T) {
	meta := swarm.Meta{Version: swarm.Version{Index: 42}}

	digests := []Digest{
		ServiceDigest(swarm.Service{ID: "s", Meta: meta}, nil, nil, nil, false, nil),
		NodeDigest(swarm.Node{ID: "n", Meta: meta}, nil, nil),
		ConfigDigest(swarm.Config{ID: "c", Meta: meta}, nil),
		SecretDigest(swarm.Secret{ID: "x", Meta: meta}, nil),
		TaskDigest(swarm.Task{ID: "t", Meta: meta}, nil, nil),
		StackDigest(cache.StackDetail{}, nil, nil),
		NetworkDigest(network.Summary{ID: "net"}, nil, nil),
		VolumeDigest(volume.Volume{Name: "v"}, nil),
	}

	for _, digest := range digests {
		want := uint64(0)
		if Versioned(digest.Type) {
			want = 42
		}

		if digest.Version != want {
			t.Errorf("%s digest version = %d, want %d", digest.Type, digest.Version, want)
		}
	}
}
