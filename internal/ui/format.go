// Package ui renders monitor data in a terminal.
package ui

import (
	"fmt"
	"math"
)

const bytesPerUnit = 1024.0

var units = [...]string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}

// FormatRate formats bytes per second using binary units.
func FormatRate(bytesPerSecond float64) string {
	return formatValue(bytesPerSecond, "/s")
}

// FormatBytes formats a byte total using binary units.
func FormatBytes(bytes uint64) string {
	unit := 0
	remaining := bytes
	for remaining >= 1024 && unit < len(units)-1 {
		remaining /= 1024
		unit++
	}

	// Convert only the selected scaled value at the presentation boundary. The
	// unit selection itself remains integer-exact for the complete uint64 range.
	divisor := uint64(1)
	for index := 0; index < unit; index++ {
		divisor *= 1024
	}
	return formatScaled(float64(bytes)/float64(divisor), unit, "")
}

func formatValue(value float64, suffix string) string {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
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

	return formatScaled(value, unit, suffix)
}

func formatScaled(value float64, unit int, suffix string) string {
	if unit < 0 {
		unit = 0
	}
	if unit >= len(units) {
		unit = len(units) - 1
	}
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Sprintf("0 %s%s", units[0], suffix)
	}
	// Promote values that round to a full unit rather than displaying an
	// unwieldy boundary value such as "1024 KiB".
	for math.Round(value) >= bytesPerUnit && unit < len(units)-1 {
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
