package cluster

// StackNamespaceLabel is the label a resource's stack membership is derived from.
const StackNamespaceLabel = "com.docker.stack.namespace"

// StackLabelDeniedError reports a relabel into a stack the caller may not write.
type StackLabelDeniedError struct {
	Stack string
}

func (e *StackLabelDeniedError) Error() string {
	return "write access denied for stack:" + e.Stack
}

// CheckStackLabel refuses labels that put a resource into a stack the caller
// may not write, because membership is what stack grants match on. Leaving a
// stack, or keeping the one it is in, needs only the write on the resource.
func CheckStackLabel(before, after map[string]string, canWrite func(resource string) bool) error {
	target := after[StackNamespaceLabel]
	if target == "" || target == before[StackNamespaceLabel] {
		return nil
	}

	if !canWrite("stack:" + target) {
		return &StackLabelDeniedError{Stack: target}
	}

	return nil
}

// GuardStackLabel applies CheckStackLabel to what a label mutator produces, so
// the check runs against the labels Docker returned rather than a cached copy.
func GuardStackLabel(
	mutate func(current map[string]string) (map[string]string, error),
	canWrite func(resource string) bool,
) func(current map[string]string) (map[string]string, error) {
	return func(current map[string]string) (map[string]string, error) {
		before := current[StackNamespaceLabel]

		next, err := mutate(current)
		if err != nil {
			return nil, err
		}

		if err := CheckStackLabel(
			map[string]string{StackNamespaceLabel: before}, next, canWrite,
		); err != nil {
			return nil, err
		}

		return next, nil
	}
}
