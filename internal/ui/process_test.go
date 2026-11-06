package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/m-malanchuk/netmeter-tui/internal/monitor"
)

func TestProcessModeToggleKeys(t *testing.T) {
	view := &App{}
	if action := view.HandleEvent(tcell.NewEventKey(tcell.KeyF2, 0, tcell.ModNone)); action != ActionToggleProcess || !view.ProcessMode() {
		t.Fatalf("F2 action/mode = %v/%v", action, view.ProcessMode())
	}
	if action := view.HandleEvent(tcell.NewEventKey(tcell.KeyRune, 'p', tcell.ModNone)); action != ActionToggleProcess || view.ProcessMode() {
		t.Fatalf("p action/mode = %v/%v", action, view.ProcessMode())
	}
}

func TestProcessFilterAcceptsToggleRune(t *testing.T) {
	view := &App{processMode: true, filterEditing: true}
	action := view.HandleEvent(tcell.NewEventKey(tcell.KeyRune, 'p', tcell.ModNone))
	if action != ActionNone || !view.ProcessMode() || string(view.filterInput) != "p" {
		t.Fatalf("filter p action/mode/input = %v/%v/%q", action, view.ProcessMode(), string(view.filterInput))
	}
}

func TestThemeCycleKeys(t *testing.T) {
	view := &App{}
	view.setTheme("adwaita-dark")
	if action := view.HandleEvent(tcell.NewEventKey(tcell.KeyRune, 't', tcell.ModNone)); action != ActionThemeNext || view.ThemeName() != "gruvbox-dark" {
		t.Fatalf("next theme action/name = %v/%q", action, view.ThemeName())
	}
	if action := view.HandleEvent(tcell.NewEventKey(tcell.KeyRune, 'T', tcell.ModShift)); action != ActionThemePrevious || view.ThemeName() != "adwaita-dark" {
		t.Fatalf("previous theme action/name = %v/%q", action, view.ThemeName())
	}
}

func TestPackControlLinesFitsWidthAndKeepsItems(t *testing.T) {
	items := []string{"←/↑ previous", "→/↓ next", "p/F2 processes", "+/- interval", "q quit"}
	lines := packControlLines(items, 24)
	joined := strings.Join(lines, " ")
	for _, line := range lines {
		if runeWidth(line) > 24 {
			t.Fatalf("line %q exceeds width", line)
		}
	}
	for _, item := range items {
		if !strings.Contains(joined, item) {
			t.Fatalf("item %q missing from %q", item, joined)
		}
	}
}

func TestPackControlLinesNeverExceedsOneCell(t *testing.T) {
	lines := packControlLines([]string{"界", "q"}, 1)
	for _, line := range lines {
		if textWidth(line) > 1 {
			t.Fatalf("line %q has width %d", line, textWidth(line))
		}
	}
}

func TestTextWidthHandlesWideAndCombiningRunes(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  int
	}{
		{name: "ascii", value: "abc", want: 3},
		{name: "cjk", value: "界面", want: 4},
		{name: "combining", value: "e\u0301", want: 1},
		{name: "emoji", value: "📈", want: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := textWidth(test.value); got != test.want {
				t.Fatalf("textWidth(%q) = %d, want %d", test.value, got, test.want)
			}
		})
	}
}

func TestTruncateUsesTerminalCells(t *testing.T) {
	if got := truncate("界面状态", 5); got != "界面…" {
		t.Fatalf("truncate wide text = %q, want %q", got, "界面…")
	}
	if got := padRight("界", 4); got != "界  " {
		t.Fatalf("padRight wide text = %q, want two trailing spaces", got)
	}
}

func TestSetInterfacesSortsNames(t *testing.T) {
	view := &App{}
	view.SetInterfaces([]monitor.InterfaceStats{{Name: "wlan0"}, {Name: "eth0"}, {Name: "lo"}})
	if got := view.names; len(got) != 3 || got[0] != "eth0" || got[1] != "lo" || got[2] != "wlan0" {
		t.Fatalf("interface names = %v, want sorted names", got)
	}
}

func TestResizeEventWithoutScreenIsSafe(t *testing.T) {
	view := &App{}
	if action := view.HandleEvent(tcell.NewEventResize(80, 24)); action != ActionResize {
		t.Fatalf("resize action = %v, want %v", action, ActionResize)
	}
}

func TestThemeNamesAreAvailable(t *testing.T) {
	for _, name := range []string{"adwaita-dark", "gruvbox-dark", "nord", "dracula", "tokyo-night", "solarized-dark"} {
		if !HasTheme(name) {
			t.Fatalf("theme %q is unavailable", name)
		}
	}
}

func TestRenderWrapsControlsAndMovesContentDown(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(32, 24)
	view := New(screen, 500*time.Millisecond, "", "nord")
	stats := monitor.InterfaceStats{Name: "eth0", Available: true}
	view.SetInterfaces([]monitor.InterfaceStats{stats})
	view.Render(stats, true, "")

	cells, width, height := screen.GetContents()
	lines := make([]string, height)
	for y := 0; y < height; y++ {
		line := make([]rune, width)
		for x := 0; x < width; x++ {
			cell := cells[y*width+x]
			line[x] = ' '
			if len(cell.Runes) > 0 {
				line[x] = cell.Runes[0]
			}
		}
		lines[y] = string(line)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "t/T theme") || !strings.Contains(joined, "q quit") {
		t.Fatalf("wrapped controls missing:\n%s", joined)
	}
	incomingLine := -1
	for index, line := range lines {
		if strings.Contains(line, "Incoming / RX") {
			incomingLine = index
			break
		}
	}
	if incomingLine <= 3 {
		t.Fatalf("content started at line %d; controls did not move it down", incomingLine)
	}
}

func TestRenderPaintsWholeCanvasBackground(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(40, 18)
	view := New(screen, 500*time.Millisecond, "", "tokyo-night")
	stats := monitor.InterfaceStats{Name: "eth0", Available: true}
	view.SetInterfaces([]monitor.InterfaceStats{stats})
	view.Render(stats, true, "")

	cells, _, _ := screen.GetContents()
	for index, cell := range cells {
		_, background, _ := cell.Style.Decompose()
		if background != view.theme.Background {
			t.Fatalf("cell %d background = %v, want %v", index, background, view.theme.Background)
		}
	}
}

func TestRenderHandlesNilAndTinyScreens(t *testing.T) {
	view := New(nil, 500*time.Millisecond, "", "nord")
	view.Render(monitor.InterfaceStats{}, false, "")

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(1, 1)
	view = New(screen, 500*time.Millisecond, "", "nord")
	view.Render(monitor.InterfaceStats{}, false, "")
}

func TestDrawTextClipsWideRuneAtRightEdge(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(1, 1)
	drawText(screen, 0, 0, tcell.StyleDefault, "界")
	cells, _, _ := screen.GetContents()
	if len(cells) != 1 || len(cells[0].Runes) != 0 {
		t.Fatalf("clipped wide rune left cell contents: %+v", cells)
	}
}

func TestVisibleProcessesSortsByRateAndUsesStablePIDTieBreak(t *testing.T) {
	view := &App{
		processSort: ProcessSortRX,
		processes: []monitor.ProcessStats{
			{PID: 20, Name: "beta", RX: monitor.Metric{Current: 100}},
			{PID: 10, Name: "alpha", RX: monitor.Metric{Current: 100}},
			{PID: 30, Name: "gamma", RX: monitor.Metric{Current: 10}},
		},
	}
	rows := view.visibleProcesses()
	if rows[0].PID != 20 || rows[1].PID != 10 || rows[2].PID != 30 {
		t.Fatalf("descending rows = %+v", rows)
	}
	view.processAscending = true
	rows = view.visibleProcesses()
	if rows[0].PID != 30 || rows[1].PID != 10 || rows[2].PID != 20 {
		t.Fatalf("ascending rows = %+v", rows)
	}
}

func TestVisibleProcessesFiltersNameAndPID(t *testing.T) {
	view := &App{
		processSort:      ProcessSortPID,
		processAscending: true,
		filter:           "42",
		processes: []monitor.ProcessStats{
			{PID: 42, Name: "worker"},
			{PID: 7, Name: "worker-42-helper"},
			{PID: 8, Name: "other"},
		},
	}
	rows := view.visibleProcesses()
	if len(rows) != 2 {
		t.Fatalf("filtered rows = %+v", rows)
	}
}

func TestProcessFilterMatch(t *testing.T) {
	process := monitor.ProcessStats{PID: 421, Name: "Firefox"}
	for _, filter := range []string{"fire", "421", "FIREFOX"} {
		if !processFilterMatch(process, filter) {
			t.Errorf("filter %q did not match", filter)
		}
	}
	if processFilterMatch(process, "chrome") {
		t.Error("unexpected match for chrome")
	}
}

func TestProcessMaximums(t *testing.T) {
	rx, tx, total, connections := processMaximums([]monitor.ProcessStats{{
		RX:          monitor.Metric{Current: 2},
		TX:          monitor.Metric{Current: 3},
		Total:       4,
		Connections: 5,
	}})
	if rx != 2 || tx != 3 || total != 4 || connections != 5 {
		t.Fatalf("maximums = %v/%v/%v/%v", rx, tx, total, connections)
	}
}

func TestProcessWindowKeepsSelectionVisible(t *testing.T) {
	rows := []monitor.ProcessStats{
		{PID: 1}, {PID: 2}, {PID: 3}, {PID: 4}, {PID: 5},
	}
	view := &App{processPID: 5}
	window := view.processWindow(rows, 2)
	if len(window) != 2 || window[0].PID != 4 || window[1].PID != 5 {
		t.Fatalf("window = %+v", window)
	}
}
