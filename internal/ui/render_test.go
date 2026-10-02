package ui

import "testing"

func TestCalculateLayoutKeepsControlsWithinWidth(t *testing.T) {
	for _, width := range []int{1, 2, 8, 20, 80} {
		for _, processMode := range []bool{false, true} {
			layout := calculateLayout(width, 24, processMode)
			for _, line := range layout.controlLines {
				if textWidth(line) > width {
					t.Fatalf("width=%d process=%v line %q exceeds width", width, processMode, line)
				}
			}
		}
	}
}

func TestCalculateLayoutHandlesTinyScreens(t *testing.T) {
	for _, dimensions := range [][2]int{{0, 0}, {1, 1}, {4, 2}, {10, 4}} {
		layout := calculateLayout(dimensions[0], dimensions[1], false)
		if dimensions[0] <= 0 || dimensions[1] <= 0 {
			if layout.controlLines != nil || layout.contentTop != 0 || layout.footerY != 0 {
				t.Fatalf("layout(%v) = %+v, want zero layout", dimensions, layout)
			}
			continue
		}
		if layout.footerY != dimensions[1]-1 {
			t.Fatalf("layout(%v).footerY = %d", dimensions, layout.footerY)
		}
	}
}
