package ui

import (
	"math"
	"testing"
)

func TestGraphHeightsUsesVisibleWindow(t *testing.T) {
	heights, maximum := GraphHeights([]float64{1, 2, 4, 8}, 3, 4)
	if maximum != 8 {
		t.Fatalf("maximum = %v, want 8", maximum)
	}
	want := []int{1, 2, 4}
	for i := range want {
		if heights[i] != want[i] {
			t.Fatalf("heights = %v, want %v", heights, want)
		}
	}
}

func TestGraphHeightsZeroAndEmpty(t *testing.T) {
	for _, samples := range [][]float64{nil, {0, 0}} {
		heights, maximum := GraphHeights(samples, 3, 4)
		if maximum != 0 || len(heights) != 3 {
			t.Fatalf("GraphHeights(%v) = heights %v max %v", samples, heights, maximum)
		}
		for _, height := range heights {
			if height != 0 {
				t.Fatalf("height = %d, want 0", height)
			}
		}
	}
}

func TestGraphHeightsIgnoresInvalidAndNegativeSamples(t *testing.T) {
	heights, maximum := GraphHeights([]float64{math.NaN(), math.Inf(1), -2, 2}, 4, 4)
	if maximum != 2 {
		t.Fatalf("maximum = %v, want 2", maximum)
	}
	want := []int{0, 0, 0, 4}
	for index, height := range want {
		if heights[index] != height {
			t.Fatalf("heights = %v, want %v", heights, want)
		}
	}
}

func TestGraphHeightsRejectsInvalidDimensions(t *testing.T) {
	for _, dimensions := range [][2]int{{0, 4}, {4, 0}, {-1, 3}, {3, -1}} {
		heights, maximum := GraphHeights([]float64{1, 2}, dimensions[0], dimensions[1])
		if heights != nil || maximum != 0 {
			t.Fatalf("GraphHeights dimensions %v = %v/%v, want nil/0", dimensions, heights, maximum)
		}
	}
}
