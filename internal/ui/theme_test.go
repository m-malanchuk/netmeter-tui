package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestThemeLookupNormalizesNames(t *testing.T) {
	if !HasTheme("  NORD ") {
		t.Fatal("trimmed, case-insensitive theme should be accepted")
	}
	if HasTheme("missing") {
		t.Fatal("unknown theme should be rejected")
	}
}

func TestThemeNamesReturnsIndependentList(t *testing.T) {
	names := ThemeNames()
	if len(names) != 6 {
		t.Fatalf("theme count = %d, want 6", len(names))
	}
	names[0] = "changed"
	if ThemeNames()[0] == "changed" {
		t.Fatal("ThemeNames exposed mutable theme storage")
	}
}

func TestThemePalettesHaveReadableContrast(t *testing.T) {
	for _, theme := range themes {
		for label, foreground := range map[string]tcell.Color{
			"foreground": theme.Foreground,
			"title":      theme.Title,
			"highlight":  theme.Highlight,
			"process":    theme.Process,
			"download":   theme.Download,
			"upload":     theme.Upload,
		} {
			if contrastRatio(foreground, theme.Background) < 1.2 {
				t.Errorf("theme %s %s contrast is too low", theme.Name, label)
			}
		}
		if contrastRatio(theme.SelectedFG, theme.SelectedBG) < 1.5 {
			t.Errorf("theme %s selected contrast is too low", theme.Name)
		}
	}
}

func contrastRatio(first, second tcell.Color) float64 {
	firstLuminance := luminance(first)
	secondLuminance := luminance(second)
	if firstLuminance < secondLuminance {
		firstLuminance, secondLuminance = secondLuminance, firstLuminance
	}
	return (firstLuminance + 0.05) / (secondLuminance + 0.05)
}

func luminance(color tcell.Color) float64 {
	r, g, b := color.RGB()
	channel := func(value int32) float64 {
		normalized := float64(value) / 255
		if normalized <= 0.03928 {
			return normalized / 12.92
		}
		return ((normalized + 0.055) / 1.055) * ((normalized + 0.055) / 1.055)
	}
	return 0.2126*channel(r) + 0.7152*channel(g) + 0.0722*channel(b)
}
