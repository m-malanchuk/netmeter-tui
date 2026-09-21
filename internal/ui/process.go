package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/m-malanchuk/netmeter-tui/internal/monitor"
)

func (a *App) handleFilterKey(event *tcell.EventKey) Action {
	switch event.Key() {
	case tcell.KeyEscape:
		a.filterEditing = false
		return ActionNone
	case tcell.KeyEnter:
		a.filter = strings.TrimSpace(string(a.filterInput))
		a.filterEditing = false
		a.ensureProcessSelection()
		return ActionNone
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(a.filterInput) > 0 {
			a.filterInput = a.filterInput[:len(a.filterInput)-1]
		}
		a.filter = string(a.filterInput)
		a.ensureProcessSelection()
		return ActionNone
	case tcell.KeyCtrlC:
		return ActionQuit
	}
	if character := event.Rune(); character >= ' ' && character != 0 {
		a.filterInput = append(a.filterInput, character)
		a.filter = string(a.filterInput)
		a.ensureProcessSelection()
	}
	return ActionNone
}

func (a *App) visibleProcesses() []monitor.ProcessStats {
	rows := make([]monitor.ProcessStats, 0, len(a.processes))
	for _, process := range a.processes {
		if processFilterMatch(process, a.filter) {
			rows = append(rows, process)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		less := processLess(rows[i], rows[j], a.processSort)
		if a.processAscending {
			return less
		}
		return processLess(rows[j], rows[i], a.processSort)
	})
	return rows
}

func processFilterMatch(process monitor.ProcessStats, filter string) bool {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return true
	}
	needle := strings.ToLower(filter)
	return strings.Contains(strings.ToLower(process.Name), needle) ||
		strings.Contains(processPID(process.PID), needle)
}

func processLess(left, right monitor.ProcessStats, column ProcessSort) bool {
	switch column {
	case ProcessSortPID:
		if left.PID != right.PID {
			return left.PID < right.PID
		}
	case ProcessSortName:
		if left.Name != right.Name {
			return strings.ToLower(left.Name) < strings.ToLower(right.Name)
		}
	case ProcessSortRX:
		if left.RX.Current != right.RX.Current {
			return left.RX.Current < right.RX.Current
		}
	case ProcessSortTX:
		if left.TX.Current != right.TX.Current {
			return left.TX.Current < right.TX.Current
		}
	case ProcessSortTotal:
		if left.Total != right.Total {
			return left.Total < right.Total
		}
	case ProcessSortConnections:
		if left.Connections != right.Connections {
			return left.Connections < right.Connections
		}
	}
	if left.PID != right.PID {
		return left.PID < right.PID
	}
	return left.Name < right.Name
}

func (a *App) moveProcessSort(direction int) {
	if direction == 0 {
		return
	}
	columnCount := int(ProcessSortConnections) + 1
	column := (int(a.processSort) + direction) % columnCount
	if column < 0 {
		column += columnCount
	}
	a.processSort = ProcessSort(column)
	a.processAscending = a.processSort == ProcessSortPID || a.processSort == ProcessSortName
}

func (a *App) moveProcess(direction int) {
	rows := a.visibleProcesses()
	if len(rows) == 0 || direction == 0 {
		return
	}
	index := 0
	for i, row := range rows {
		if row.PID == a.processPID && row.StartTime == a.processStartTime {
			index = i
			break
		}
	}
	index = (index + direction + len(rows)) % len(rows)
	a.processPID = rows[index].PID
	a.processStartTime = rows[index].StartTime
}

func (a *App) ensureProcessSelection() {
	rows := a.visibleProcesses()
	if len(rows) == 0 {
		a.processPID = 0
		a.processStartTime = 0
		return
	}
	for _, row := range rows {
		if row.PID == a.processPID && row.StartTime == a.processStartTime {
			return
		}
	}
	a.processPID = rows[0].PID
	a.processStartTime = rows[0].StartTime
}

func (a *App) processSortLabel() string {
	switch a.processSort {
	case ProcessSortPID:
		return "PID"
	case ProcessSortName:
		return "Process"
	case ProcessSortRX:
		return "RX/s"
	case ProcessSortTX:
		return "TX/s"
	case ProcessSortTotal:
		return "Total"
	case ProcessSortConnections:
		return "Connections"
	default:
		return "RX/s"
	}
}

func (a *App) sortDirection() string {
	if a.processAscending {
		return "↑"
	}
	return "↓"
}

func processMaximums(rows []monitor.ProcessStats) (float64, float64, float64, float64) {
	var rx, tx, total, connections float64
	for _, row := range rows {
		rx = max(rx, row.RX.Current)
		tx = max(tx, row.TX.Current)
		total = max(total, float64(row.Total))
		connections = max(connections, float64(row.Connections))
	}
	return rx, tx, total, connections
}

func (a *App) drawMetricBar(x, y, width int, value, maximum float64, label string, style tcell.Style) {
	if width <= 0 {
		return
	}
	if value < 0 || mathIsNaN(value) {
		value = 0
	}
	if maximum <= 0 || mathIsNaN(maximum) {
		maximum = 1
	}
	ratio := value / maximum
	if ratio > 1 {
		ratio = 1
	}
	fill := int(ratio * float64(width))
	fillColor, _, _ := style.Decompose()
	filledStyle := a.defaultStyle.Background(fillColor)
	for column := 0; column < width; column++ {
		cellStyle := a.emptyBarStyle
		if column < fill {
			cellStyle = filledStyle
		}
		a.screen.SetContent(x+column, y, ' ', nil, cellStyle)
	}
	label = truncate(label, width)
	labelX := x + width - runeWidth(label)
	for index, character := range []rune(label) {
		column := labelX - x + index
		cellStyle := a.emptyBarStyle.Foreground(a.theme.Foreground)
		if column >= 0 && column < fill {
			cellStyle = filledStyle.Foreground(a.theme.Background)
		}
		a.screen.SetContent(labelX+index, y, character, nil, cellStyle)
	}
}

func processPID(pid int) string {
	if pid <= 0 {
		return "?"
	}
	return fmt.Sprintf("%d", pid)
}

func padRight(value string, width int) string {
	if width <= runeWidth(value) {
		return value
	}
	return value + strings.Repeat(" ", width-runeWidth(value))
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return string(runes[:1])
	}
	return string(runes[:width-1]) + "…"
}

func runeWidth(value string) int {
	return len([]rune(value))
}

func screenWidth(screen tcell.Screen) int {
	width, _ := screen.Size()
	return width
}

func max(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func mathIsNaN(value float64) bool {
	return value != value
}
