package ui

import "testing"

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
