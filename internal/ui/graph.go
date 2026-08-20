package ui

import "math"

// GraphHeights maps the newest samples to integer bar heights. It returns the
// visible maximum as the second result so callers can display the graph scale.
func GraphHeights(samples []float64, width, height int) ([]int, float64) {
	if width <= 0 || height <= 0 {
		return nil, 0
	}
	visible := samples
	if len(visible) > width {
		visible = visible[len(visible)-width:]
	}
	heights := make([]int, width)
	var maximum float64
	for _, sample := range visible {
		if sample > maximum && !math.IsNaN(sample) {
			maximum = sample
		}
	}
	if maximum <= 0 || math.IsNaN(maximum) {
		return heights, 0
	}

	start := width - len(visible)
	for i, sample := range visible {
		if sample <= 0 || math.IsNaN(sample) {
			continue
		}
		barHeight := int(math.Round(sample / maximum * float64(height)))
		if barHeight < 0 {
			barHeight = 0
		}
		if barHeight > height {
			barHeight = height
		}
		heights[start+i] = barHeight
	}
	return heights, maximum
}
