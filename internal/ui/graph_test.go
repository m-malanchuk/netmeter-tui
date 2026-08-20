package ui

import "testing"

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
