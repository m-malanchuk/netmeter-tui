package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/m-malanchuk/netmeter-tui/internal/monitor"
)

// Render draws the current view. selectedOK is false when the requested
// interface is temporarily absent.
func (a *App) Render(selected monitor.InterfaceStats, selectedOK bool, status string) {
	a.screen.SetStyle(a.defaultStyle)
	// tcell's Clear always uses StyleDefault, regardless of SetStyle. Fill the
	// complete canvas explicitly so blank cells use the active theme as well.
	a.screen.Fill(' ', a.defaultStyle)
	width, height := a.screen.Size()
	if width <= 0 || height <= 0 {
		a.screen.Show()
		return
	}

	header := fmt.Sprintf("NetMeter TUI  mode: Dashboard (p/F2: processes)  interface: %s  interval: %s  theme: %s", displayName(a.selected), a.interval, a.theme.Name)
	if a.processMode {
		header = fmt.Sprintf("NetMeter TUI  mode: Processes (p/F2: dashboard)  interface: %s  sort: %s %s  interval: %s  theme: %s", displayName(a.selected), a.processSortLabel(), a.sortDirection(), a.interval, a.theme.Name)
	}
	if a.paused {
		header += "  [PAUSED]"
	}
	drawText(a.screen, 0, 0, a.headerStyle, header)
	controls := []string{"←/↑ previous", "→/↓ next", "p/F2 processes", "+/- interval", "Space pause", "r reset", "t/T theme", "q quit"}
	if a.processMode {
		controls = []string{"↑/↓ row", "←/→ or Tab sort", "Enter order", ",/. interface", "/ filter", "p/F2 dashboard", "+/- interval", "Space pause", "r reset", "t/T theme", "q quit"}
	}
	controlLines := packControlLines(controls, width)
	for index, line := range controlLines {
		drawText(a.screen, 0, 1+index, a.mutedStyle, line)
	}
	if 1+len(controlLines) >= height {
		a.screen.Show()
		return
	}

	contentTop := 2 + len(controlLines)
	footerY := height - 1
	if contentTop >= footerY {
		drawText(a.screen, 0, footerY, a.statusStyle, "Terminal too small; resize to continue")
		a.screen.Show()
		return
	}
	contentHeight := footerY - contentTop
	sectionHeight := contentHeight / 2
	if sectionHeight < 2 {
		drawText(a.screen, 0, contentTop, a.mutedStyle, "Terminal too small; resize to continue")
	} else if a.selected == "" {
		drawText(a.screen, 0, contentTop, a.mutedStyle, "No network interfaces available; waiting for discovery")
	} else if !selectedOK || !selected.Available {
		drawText(a.screen, 0, contentTop, a.mutedStyle, fmt.Sprintf("Interface %s is unavailable; waiting for it to return", displayName(a.selected)))
	} else if a.processMode {
		a.drawProcesses(contentTop, contentHeight, selectedOK)
	} else {
		a.drawSection(contentTop, sectionHeight, "Incoming / RX", selected.RX, selected.RXHistory, a.incomingStyle)
		a.drawSection(contentTop+sectionHeight, contentHeight-sectionHeight, "Outgoing / TX", selected.TX, selected.TXHistory, a.outgoingStyle)
	}

	if status == "" {
		if a.paused {
			status = "Paused — press Space to resume"
		} else {
			status = "Ready"
		}
	}
	drawText(a.screen, 0, footerY, a.statusStyle, status)
	a.screen.Show()
}

func (a *App) drawProcesses(y, height int, selectedOK bool) {
	if !selectedOK || a.selected == "" {
		drawText(a.screen, 0, y, a.mutedStyle, "Select an available interface to inspect process traffic")
		return
	}
	if a.processLoading {
		drawText(a.screen, 0, y, a.mutedStyle, fmt.Sprintf("Collecting process data for interface %s...", displayName(a.selected)))
		return
	}
	rows := a.visibleProcesses()
	if len(rows) == 0 {
		drawText(a.screen, 0, y, a.mutedStyle, fmt.Sprintf("No TCP/UDP sockets currently attributed to interface %s", displayName(a.selected)))
		return
	}
	width, _ := a.screen.Size()
	if height < 3 {
		return
	}
	drawText(a.screen, 0, y, a.headerStyle, "PID     Process          RX/s                  TX/s                  Total              Conns")
	rowY := y + 1
	maxRows := height - 1
	if a.filter != "" || a.filterEditing {
		maxRows--
	}
	if maxRows < 0 {
		maxRows = 0
	}
	if maxRows > len(rows) {
		maxRows = len(rows)
	}
	nameWidth := 16
	if width < 100 {
		nameWidth = 12
	}
	fixed := 7 + nameWidth
	metricWidth := (width - fixed) / 4
	if metricWidth < 8 {
		a.drawCompactProcesses(y, height, rows)
		return
	}
	maxRX, maxTX, maxTotal, maxConnections := processMaximums(rows)
	visibleRows := a.processWindow(rows, maxRows)
	for index, row := range visibleRows {
		style := a.defaultStyle
		if row.PID == a.processPID && row.StartTime == a.processStartTime {
			style = a.selectedStyle
		}
		drawText(a.screen, 0, rowY+index, style, fmt.Sprintf("%6s ", processPID(row.PID)))
		drawText(a.screen, 7, rowY+index, style, padRight(truncate(row.Name, nameWidth), nameWidth))
		x := fixed
		a.drawMetricBar(x, rowY+index, metricWidth, row.RX.Current, maxRX, FormatRate(row.RX.Current), a.incomingStyle)
		x += metricWidth
		a.drawMetricBar(x, rowY+index, metricWidth, row.TX.Current, maxTX, FormatRate(row.TX.Current), a.outgoingStyle)
		x += metricWidth
		a.drawMetricBar(x, rowY+index, metricWidth, float64(row.Total), maxTotal, FormatBytes(row.Total), a.processStyle)
		x += metricWidth
		a.drawMetricBar(x, rowY+index, metricWidth, float64(row.Connections), maxConnections, fmt.Sprintf("%d", row.Connections), a.headerStyle)
	}
	a.drawFilter(y, height)
}

func (a *App) drawCompactProcesses(y, height int, rows []monitor.ProcessStats) {
	maxRX, maxTX, maxTotal, maxConnections := processMaximums(rows)
	width := screenWidth(a.screen)
	metricWidth := maxInt(6, width/4)
	availableHeight := height
	if a.filter != "" || a.filterEditing {
		availableHeight--
	}
	rows = a.processWindow(rows, availableHeight/2)
	rowY := y
	for _, row := range rows {
		if rowY+1 >= y+height {
			break
		}
		style := a.defaultStyle
		if row.PID == a.processPID && row.StartTime == a.processStartTime {
			style = a.selectedStyle
		}
		drawText(a.screen, 0, rowY, style, fmt.Sprintf("%s %s", processPID(row.PID), truncate(row.Name, 18)))
		rowY++
		values := []struct {
			value float64
			max   float64
			text  string
			color tcell.Style
		}{
			{row.RX.Current, maxRX, "RX " + FormatRate(row.RX.Current), a.incomingStyle},
			{row.TX.Current, maxTX, "TX " + FormatRate(row.TX.Current), a.outgoingStyle},
			{float64(row.Total), maxTotal, "Total " + FormatBytes(row.Total), a.processStyle},
			{float64(row.Connections), maxConnections, fmt.Sprintf("Conns %d", row.Connections), a.headerStyle},
		}
		x := 0
		for _, value := range values {
			if x >= width {
				break
			}
			cellWidth := metricWidth
			if x+cellWidth > width {
				cellWidth = width - x
			}
			a.drawMetricBar(x, rowY, cellWidth, value.value, value.max, value.text, value.color)
			x += metricWidth
		}
		rowY++
	}
	a.drawFilter(y, height)
}

func (a *App) drawFilter(y, height int) {
	if a.filter == "" && !a.filterEditing {
		return
	}
	filterText := a.filter
	if a.filterEditing {
		filterText = string(a.filterInput)
	}
	drawText(a.screen, 0, y+height-1, a.statusStyle, "filter: "+filterText)
}

func (a *App) processWindow(rows []monitor.ProcessStats, capacity int) []monitor.ProcessStats {
	if capacity <= 0 {
		return nil
	}
	if len(rows) <= capacity {
		return rows
	}
	selected := 0
	for index, row := range rows {
		if row.PID == a.processPID && row.StartTime == a.processStartTime {
			selected = index
			break
		}
	}
	start := selected - capacity + 1
	if start < 0 {
		start = 0
	}
	if start+capacity > len(rows) {
		start = len(rows) - capacity
	}
	return rows[start : start+capacity]
}

func (a *App) drawSection(y, height int, title string, metric monitor.Metric, history []float64, style tcell.Style) {
	if height <= 0 {
		return
	}
	width, _ := a.screen.Size()
	graphHeight := height - 3
	heights, graphMaximum := GraphHeights(history, width, graphHeight)
	maxText := FormatRate(graphMaximum)
	drawText(a.screen, 0, y, style.Bold(true), fmt.Sprintf("%s  (window max %s)", title, maxText))
	if height == 1 {
		return
	}
	drawText(a.screen, 0, y+1, a.defaultStyle, fmt.Sprintf("Current %-14s Average %s", FormatRate(metric.Current), FormatRate(metric.Average)))
	if height == 2 {
		return
	}
	drawText(a.screen, 0, y+2, a.defaultStyle, fmt.Sprintf("Maximum %-14s Total %-14s Session %s", FormatRate(metric.Maximum), FormatBytes(metric.Total), FormatBytes(metric.SessionTotal)))
	for column, barHeight := range heights {
		for row := 0; row < graphHeight; row++ {
			character := '·'
			barStyle := a.mutedStyle
			if graphHeight-row <= barHeight {
				character = '█'
				barStyle = style
			}
			a.screen.SetContent(column, y+3+row, character, nil, barStyle)
		}
	}
}
