package compose

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// A field quietly changing shape is what a whole-document diff catches and an
// assertion on one field does not.
func TestGoldens(t *testing.T) {
	for name, build := range map[string]func() (File, []string){
		"stack":   func() (File, []string) { return FromStack(testStack(), clusterNetworks()) },
		"service": func() (File, []string) { return FromService(testService(), clusterNetworks()) },
	} {
		t.Run(name, func(t *testing.T) {
			f, warnings := build()
			got, err := Render(f, warnings)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}

			path := filepath.Join("testdata", name+".yaml")
			if *update {
				if err := os.WriteFile(path, got, 0o600); err != nil {
					t.Fatalf("write golden: %v", err)
				}

				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v (run with -update to create it)", err)
			}
			if string(got) != string(want) {
				t.Errorf(
					"document differs from %s:\n--- got ---\n%s\n--- want ---\n%s",
					path, got, want,
				)
			}
		})
	}
}
