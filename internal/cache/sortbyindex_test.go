package cache

import (
	"cmp"
	"slices"
	"testing"
)

func TestSortByIndexMatchesSlicesSort(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3, 17, 500} {
		vals := make([]int, n)
		for i := range vals {
			vals[i] = (i*7919 + 13) % 50 // scrambled, with repeats
		}

		want := slices.Clone(vals)
		slices.Sort(want)

		sortByIndex(vals, func(a, b int) int { return cmp.Compare(vals[a], vals[b]) })

		if !slices.Equal(vals, want) {
			t.Errorf("n=%d: got %v, want %v", n, vals, want)
		}
	}
}
