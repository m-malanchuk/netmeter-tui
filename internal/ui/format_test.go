package ui

import (
	"math"
	"testing"
)

func TestFormatRateUsesBinaryUnits(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		want  string
	}{
		{name: "zero", value: 0, want: "0 B/s"},
		{name: "bytes", value: 999, want: "999 B/s"},
		{name: "kibibyte", value: 1024, want: "1.00 KiB/s"},
		{name: "mebibyte", value: 15326.45 * 1024, want: "14.97 MiB/s"},
		{name: "gibibyte", value: 1024 * 1024 * 1024, want: "1.00 GiB/s"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := FormatRate(test.value); got != test.want {
				t.Fatalf("FormatRate(%v) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		value uint64
		want  string
	}{
		{value: 0, want: "0 B"},
		{value: 1024, want: "1.00 KiB"},
		{value: 1024 * 1024, want: "1.00 MiB"},
		{value: 1024 * 1024 * 1024, want: "1.00 GiB"},
		{value: 1024 * 1024 * 1024 * 1024, want: "1.00 TiB"},
	}
	for _, test := range tests {
		if got := FormatBytes(test.value); got != test.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", test.value, got, test.want)
		}
	}
}

func TestFormatRateRejectsInvalidValues(t *testing.T) {
	for _, value := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if got := FormatRate(value); got != "0 B/s" {
			t.Errorf("FormatRate(%v) = %q, want zero", value, got)
		}
	}
}

func TestFormatBytesKeepsExactUnitSelectionAtLargeValues(t *testing.T) {
	tests := []struct {
		value uint64
		want  string
	}{
		{value: uint64(1) << 50, want: "1.00 PiB"},
		{value: uint64(1) << 60, want: "1.00 EiB"},
		{value: ^uint64(0), want: "16.00 EiB"},
	}
	for _, test := range tests {
		if got := FormatBytes(test.value); got != test.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", test.value, got, test.want)
		}
	}
}

func TestFormatBytesPromotesRoundedUnitBoundary(t *testing.T) {
	if got := FormatBytes((uint64(1) << 20) - 1); got != "1.00 MiB" {
		t.Fatalf("FormatBytes(2^20-1) = %q, want promoted MiB", got)
	}
}
