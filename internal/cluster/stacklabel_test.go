package cluster

import (
	"errors"
	"testing"
)

func writableStacks(stacks ...string) func(string) bool {
	return func(resource string) bool {
		for _, s := range stacks {
			if resource == "stack:"+s {
				return true
			}
		}
		return false
	}
}

func in(stack string) map[string]string {
	return map[string]string{StackNamespaceLabel: stack}
}

func TestCheckStackLabel(t *testing.T) {
	tests := []struct {
		name          string
		before, after map[string]string
		wantDenied    string
	}{
		{"unchanged", in("globex"), map[string]string{StackNamespaceLabel: "globex", "a": "b"}, ""},
		{"move into unwritable stack", in("acme"), in("globex"), "globex"},
		{"join unwritable stack", nil, in("globex"), "globex"},
		{"move into writable stack", in("globex"), in("acme"), ""},
		{"leave stack", in("acme"), map[string]string{}, ""},
		{"empty value", nil, in(""), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckStackLabel(tt.before, tt.after, writableStacks("acme"))

			if tt.wantDenied == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if denied, ok := errors.AsType[*StackLabelDeniedError](err); !ok ||
				denied.Stack != tt.wantDenied {
				t.Fatalf("got %v, want denial for %q", err, tt.wantDenied)
			}
		})
	}
}

func TestGuardStackLabel_ComparesAgainstLabelsBeforeTheMutation(t *testing.T) {
	// A mutator that edits the map in place must not hide the move.
	inPlace := func(current map[string]string) (map[string]string, error) {
		current[StackNamespaceLabel] = "globex"
		return current, nil
	}

	_, err := GuardStackLabel(inPlace, writableStacks("acme"))(
		map[string]string{StackNamespaceLabel: "acme"},
	)

	if _, ok := errors.AsType[*StackLabelDeniedError](err); !ok {
		t.Fatalf("got %v, want StackLabelDeniedError", err)
	}
}
