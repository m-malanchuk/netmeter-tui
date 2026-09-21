package ui

import (
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/m-malanchuk/netmeter-tui/internal/monitor"
)

// App owns terminal rendering and keyboard selection state.
type App struct {
	screen           tcell.Screen
	interval         time.Duration
	fixed            string
	selected         string
	names            []string
	paused           bool
	processMode      bool
	processLoading   bool
	processes        []monitor.ProcessStats
	processSort      ProcessSort
	processAscending bool
	processPID       int
	processStartTime uint64
	filter           string
	filterInput      []rune
	filterEditing    bool
	themeIndex       int
	theme            themePalette
	defaultStyle     tcell.Style
	headerStyle      tcell.Style
	incomingStyle    tcell.Style
	outgoingStyle    tcell.Style
	mutedStyle       tcell.Style
	statusStyle      tcell.Style
	processStyle     tcell.Style
	selectedStyle    tcell.Style
	emptyBarStyle    tcell.Style
}

// ProcessSort is a sortable process-table column.
type ProcessSort uint8

const (
	ProcessSortPID ProcessSort = iota
	ProcessSortName
	ProcessSortRX
	ProcessSortTX
	ProcessSortTotal
	ProcessSortConnections
)

// Action describes a user interaction that the application loop should handle.
type Action uint8

const (
	ActionNone Action = iota
	ActionQuit
	ActionPrevious
	ActionNext
	ActionTogglePause
	ActionReset
	ActionResize
	ActionIntervalIncrease
	ActionIntervalDecrease
	ActionToggleProcess
	ActionThemeNext
	ActionThemePrevious
)

// New creates a terminal view. fixed is empty in automatic discovery mode.
func New(screen tcell.Screen, interval time.Duration, fixed, themeName string) *App {
	app := &App{screen: screen, interval: interval, fixed: fixed, processSort: ProcessSortRX}
	app.setTheme(themeName)
	return app
}

// SetInterfaces updates the navigable interface list while preserving selection
// by name where possible.
func (a *App) SetInterfaces(interfaces []monitor.InterfaceStats) {
	a.names = a.names[:0]
	for _, stats := range interfaces {
		a.names = append(a.names, stats.Name)
	}
	if a.fixed != "" {
		a.selected = a.fixed
		return
	}
	if contains(a.names, a.selected) {
		return
	}
	if len(a.names) == 0 {
		a.selected = ""
		return
	}
	a.selected = a.names[0]
}

// Selected returns the currently selected interface name.
func (a *App) Selected() string {
	return a.selected
}

// ProcessMode reports whether the process table is active.
func (a *App) ProcessMode() bool {
	return a.processMode
}

// SetProcesses updates process rows while preserving the selected process.
func (a *App) SetProcesses(processes []monitor.ProcessStats) {
	a.processes = append(a.processes[:0], processes...)
	a.ensureProcessSelection()
}

// SetProcessLoading controls the transient state shown while a fresh process
// snapshot is collected for a newly selected interface.
func (a *App) SetProcessLoading(loading bool) {
	a.processLoading = loading
}

// HandleEvent applies one terminal event and returns the resulting action.
func (a *App) HandleEvent(event tcell.Event) Action {
	switch event := event.(type) {
	case *tcell.EventKey:
		if a.filterEditing {
			return a.handleFilterKey(event)
		}
		if event.Key() == tcell.KeyF2 || event.Rune() == 'p' || event.Rune() == 'P' {
			a.processMode = !a.processMode
			return ActionToggleProcess
		}
		switch event.Key() {
		case tcell.KeyCtrlC:
			return ActionQuit
		case tcell.KeyLeft:
			if a.processMode {
				if event.Modifiers()&tcell.ModShift != 0 {
					a.move(-1)
					return ActionPrevious
				}
				a.moveProcessSort(-1)
				return ActionNone
			}
			a.move(-1)
			return ActionPrevious
		case tcell.KeyRight:
			if a.processMode {
				if event.Modifiers()&tcell.ModShift != 0 {
					a.move(1)
					return ActionNext
				}
				a.moveProcessSort(1)
				return ActionNone
			}
			a.move(1)
			return ActionNext
		case tcell.KeyUp:
			if a.processMode {
				a.moveProcess(-1)
				return ActionNone
			}
			a.move(-1)
			return ActionPrevious
		case tcell.KeyDown:
			if a.processMode {
				a.moveProcess(1)
				return ActionNone
			}
			a.move(1)
			return ActionNext
		case tcell.KeyTab:
			if a.processMode {
				a.moveProcessSort(1)
				return ActionNone
			}
		case tcell.KeyBacktab:
			if a.processMode {
				a.moveProcessSort(-1)
				return ActionNone
			}
		case tcell.KeyEnter:
			if a.processMode {
				a.processAscending = !a.processAscending
				return ActionNone
			}
		case tcell.KeyEscape:
			if a.processMode {
				a.processMode = false
				return ActionToggleProcess
			}
		}
		switch event.Rune() {
		case 'q', 'Q':
			return ActionQuit
		case '/':
			if a.processMode {
				a.filterEditing = true
				a.filterInput = []rune(a.filter)
				return ActionNone
			}
		case ',', '<':
			if a.processMode {
				a.move(-1)
				return ActionPrevious
			}
		case '.', '>':
			if a.processMode {
				a.move(1)
				return ActionNext
			}
		case ' ':
			a.paused = !a.paused
			return ActionTogglePause
		case 'r', 'R':
			return ActionReset
		case '+', '=':
			return ActionIntervalIncrease
		case '-', '_':
			return ActionIntervalDecrease
		case 't':
			a.cycleTheme(1)
			return ActionThemeNext
		case 'T':
			a.cycleTheme(-1)
			return ActionThemePrevious
		}
	case *tcell.EventResize:
		a.screen.Sync()
		return ActionResize
	case *tcell.EventInterrupt:
		return ActionQuit
	}
	return ActionNone
}

// Paused reports whether the display is paused.
func (a *App) Paused() bool {
	return a.paused
}

// SetInterval updates the interval shown in the header.
func (a *App) SetInterval(interval time.Duration) {
	a.interval = interval
}

// ThemeName returns the active built-in theme name.
func (a *App) ThemeName() string {
	return a.theme.Name
}

func (a *App) setTheme(name string) {
	index, found := findTheme(name)
	if !found {
		index = 0
	}
	a.themeIndex = index
	a.theme = themes[index]
	base := tcell.StyleDefault.Background(a.theme.Background)
	a.defaultStyle = base.Foreground(a.theme.Foreground)
	a.headerStyle = base.Foreground(a.theme.Title).Bold(true)
	a.incomingStyle = base.Foreground(a.theme.Download)
	a.outgoingStyle = base.Foreground(a.theme.Upload)
	a.mutedStyle = base.Foreground(a.theme.Inactive)
	a.statusStyle = base.Foreground(a.theme.Highlight)
	a.processStyle = base.Foreground(a.theme.Process)
	a.selectedStyle = base.Foreground(a.theme.SelectedFG).Background(a.theme.SelectedBG)
	a.emptyBarStyle = base.Foreground(a.theme.Inactive).Background(a.theme.SelectedBG)
	if a.screen != nil {
		a.screen.SetStyle(a.defaultStyle)
	}
}

func (a *App) cycleTheme(direction int) {
	if direction == 0 || len(themes) == 0 {
		return
	}
	index := (a.themeIndex + direction) % len(themes)
	if index < 0 {
		index += len(themes)
	}
	a.setTheme(themes[index].Name)
}

func packControlLines(items []string, width int) []string {
	if width <= 0 {
		return nil
	}
	lines := make([]string, 0, len(items))
	current := ""
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		candidate := item
		if current != "" {
			candidate = current + "   " + item
		}
		if runeWidth(candidate) <= width {
			current = candidate
			continue
		}
		if current != "" {
			lines = append(lines, current)
			current = ""
		}
		for runeWidth(item) > width {
			runes := []rune(item)
			lines = append(lines, string(runes[:width]))
			item = string(runes[width:])
		}
		current = item
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func (a *App) move(direction int) {
	if a.fixed != "" || len(a.names) == 0 {
		return
	}
	index := 0
	for i, name := range a.names {
		if name == a.selected {
			index = i
			break
		}
	}
	index = (index + direction + len(a.names)) % len(a.names)
	a.selected = a.names[index]
}

func displayName(name string) string {
	if name == "" {
		return "(none)"
	}
	return name
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func drawText(screen tcell.Screen, x, y int, style tcell.Style, text string) {
	width, height := screen.Size()
	if y < 0 || y >= height {
		return
	}
	column := x
	for _, character := range []rune(text) {
		if column >= width {
			break
		}
		if column >= 0 {
			screen.SetContent(column, y, character, nil, style)
		}
		column++
	}
}
