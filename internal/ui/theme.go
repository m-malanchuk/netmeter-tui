package ui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
)

// themePalette maps the subset of btop++ theme colors used by the application.
// Palettes are embedded so netmeter-tui does not depend on a local btop++ install.
type themePalette struct {
	Name       string
	Background tcell.Color
	Foreground tcell.Color
	Title      tcell.Color
	Highlight  tcell.Color
	SelectedBG tcell.Color
	SelectedFG tcell.Color
	Inactive   tcell.Color
	Process    tcell.Color
	Download   tcell.Color
	Upload     tcell.Color
}

var themes = []themePalette{
	{
		Name: "adwaita-dark", Background: color(0x1d1d1d), Foreground: color(0xdeddda), Title: color(0xdeddda),
		Highlight: color(0x62a0ea), SelectedBG: color(0x1c71d8), SelectedFG: color(0xffffff), Inactive: color(0x77767b),
		Process: color(0x1a5fb4), Download: color(0x1c71d8), Upload: color(0xc01b24),
	},
	{
		Name: "gruvbox-dark", Background: color(0x1d2021), Foreground: color(0xa89984), Title: color(0xebdbb2),
		Highlight: color(0xd79921), SelectedBG: color(0x282828), SelectedFG: color(0xfabd2f), Inactive: color(0x585858),
		Process: color(0x98971a), Download: color(0x6c71c4), Upload: color(0xb16286),
	},
	{
		Name: "nord", Background: color(0x2e3440), Foreground: color(0xd8dee9), Title: color(0x8fbcbb),
		Highlight: color(0x5e81ac), SelectedBG: color(0x4c566a), SelectedFG: color(0xeceff4), Inactive: color(0x4c566a),
		Process: color(0x5e81ac), Download: color(0x88c0d0), Upload: color(0x81a1c1),
	},
	{
		Name: "dracula", Background: color(0x282a36), Foreground: color(0xf8f8f2), Title: color(0xf8f8f2),
		Highlight: color(0x6272a4), SelectedBG: color(0xff79c6), SelectedFG: color(0xf8f8f2), Inactive: color(0x44475a),
		Process: color(0xbd93f9), Download: color(0x50fa7b), Upload: color(0xff79c6),
	},
	{
		Name: "tokyo-night", Background: color(0x1a1b26), Foreground: color(0xcfc9c2), Title: color(0xcfc9c2),
		Highlight: color(0x7dcfff), SelectedBG: color(0x414868), SelectedFG: color(0xcfc9c2), Inactive: color(0x565f89),
		Process: color(0x7dcfff), Download: color(0x9ece6a), Upload: color(0xf7768e),
	},
	{
		Name: "solarized-dark", Background: color(0x002b36), Foreground: color(0xeee8d5), Title: color(0xfdf6e3),
		Highlight: color(0xb58900), SelectedBG: color(0x073642), SelectedFG: color(0xd6a200), Inactive: color(0x586e75),
		Process: color(0xbad600), Download: color(0x6c71c4), Upload: color(0xd33682),
	},
}

func color(value int32) tcell.Color {
	return tcell.NewHexColor(value)
}

// ThemeNames returns the accepted CLI theme names in TUI cycle order.
func ThemeNames() []string {
	names := make([]string, len(themes))
	for index, theme := range themes {
		names[index] = theme.Name
	}
	return names
}

// HasTheme reports whether name identifies a built-in theme.
func HasTheme(name string) bool {
	_, found := findTheme(name)
	return found
}

func findTheme(name string) (int, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for index, theme := range themes {
		if theme.Name == name {
			return index, true
		}
	}
	return 0, false
}
