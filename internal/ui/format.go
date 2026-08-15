// Package ui renders monitor data in a terminal.
package ui

import (
	"fmt"
	"math"
)

const bytesPerUnit = 1024.0

var units = [...]string{"B", "KiB", "MiB", "GiB", "TiB"}

// FormatRate formats bytes per second using binary units.
func FormatRate(bytesPerSecond float64) string {
	return formatValue(bytesPerSecond, "/s")
}

// FormatBytes formats a byte total using binary units.
func FormatBytes(bytes uint64) string {
	return formatValue(float64(bytes), "")
}

func formatValue(value float64, suffix string) string {
	if math.IsNaN(value) || value < 0 {
		value = 0
	}
	if value == 0 {
		return fmt.Sprintf("0 %s%s", units[0], suffix)
	}
	unit := 0
	for value >= bytesPerUnit && unit < len(units)-1 {
		value /= bytesPerUnit
		unit++
	}

	if unit == 0 && suffix == "" {
		return fmt.Sprintf("%.0f %s", value, units[unit])
	}
	if value >= 100 {
		return fmt.Sprintf("%.0f %s%s", value, units[unit], suffix)
	}
	return fmt.Sprintf("%.2f %s%s", value, units[unit], suffix)
}
