package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"ytd/internal/format"
)

var statusStyles = map[format.Status]lipgloss.Style{
	format.Loaded:      lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
	format.Downloading: lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
	format.Done:        lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
	format.Error:       lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
	format.Cancelled:   lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
}

var (
	headerStyle    = lipgloss.NewStyle().Background(lipgloss.Color("4")).Foreground(lipgloss.Color("15")).Bold(true)
	footerStyle    = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("7"))
	cursorOnStyle  = lipgloss.NewStyle().Background(lipgloss.Color("4")).Foreground(lipgloss.Color("0")).Bold(true)
	cursorOffStyle = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("15")).Bold(true)
	selectionMark  = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	borderIdle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	borderFocus    = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)
)

const (
	queueMaxRows = 8
	// minLogWidth and minLogHeight are the smallest terminal that still fits
	// the vertical log panel; below either one it hides itself.
	minLogWidth  = 76
	minLogHeight = 24
)

// contentWidth is the main column's width, excluding the vertical log panel.
func (m *Model) contentWidth() int {
	if m.bodyWidth > 0 {
		return m.bodyWidth
	}
	return m.width
}

// panelInner is the usable width inside a bordered panel.
func (m *Model) panelInner() int { return max(8, m.contentWidth()-2) }

// View renders the whole screen: a header, a URL field, and a body split
// between the queue/format explorer and, when open, a vertical log panel.
func (m *Model) View() string {
	if m.editor != nil {
		return m.editorView()
	}
	if m.width <= 0 {
		return ""
	}
	H, W := m.height, m.width

	job := m.focusedJob()
	sideLog := m.showLog && m.logFits()
	logWidth := 0
	if sideLog {
		logWidth = clamp(W/3, 30, 46)
	}
	m.bodyWidth = max(24, W-logWidth)

	// Header (1) + URL box (3) + footer (1) are fixed chrome.
	bodyH := max(4, H-5)

	reportLines := m.reportLineBudget(job)
	slots := true

	// The progressive table stacks under the video table only when both have
	// rows; the extra borders and header come out of the formats area so the
	// body height does not move when a row has muxed streams.
	stacked := len(m.muxed) > 0 && len(m.visible) > 0
	extraBox := 0
	if stacked {
		extraBox = 3
	}

	// Rows spent on chrome inside the main column: meta, filter, both boxes'
	// borders and column headers, plus the optional slots line.
	chrome := func() int {
		count := 8 + extraBox // meta + filter + queue box (3) + formats box (3)
		if slots {
			count++
		}
		return count + reportLines
	}
	dataRows := bodyH - chrome()
	if dataRows < 2 && reportLines > 0 {
		reportLines = 0
		dataRows = bodyH - chrome()
	}
	if dataRows < 2 && slots {
		slots = false
		dataRows = bodyH - chrome()
	}
	dataRows = max(2, dataRows)

	queueWanted := len(m.jobs)
	if queueWanted == 0 {
		queueWanted = 1 // the empty placeholder still needs one row
	}
	queueRows := min(queueWanted, queueMaxRows)
	queueRows = min(queueRows, max(1, dataRows-1))
	formatRows := max(1, dataRows-queueRows)
	if leftover := bodyH - chrome() - queueRows - formatRows; leftover > 0 {
		formatRows += leftover
	}
	muxedRows := 0
	if len(m.muxed) > 0 {
		muxedRows = formatRows
		if stacked {
			muxedRows = max(1, (formatRows-3)/2)
			formatRows = max(1, formatRows-3-muxedRows)
		}
	}

	lines := make([]string, 0, H)
	lines = append(lines, m.headerView())
	lines = append(lines, m.urlBox()...)
	m.hit.url = 2
	bodyTop := len(lines)

	main := make([]string, 0, bodyH)
	queueBody, queueWindow := m.queuePanel(queueRows)
	queueTop := len(main)
	main = append(main, boxLines(m.panelTitle("queue", len(m.jobs)), queueBody, m.contentWidth(), m.area == areaQueue)...)
	m.hit.queue = panelHit{top: bodyTop + queueTop + 2, rows: queueWindow.rows, start: queueWindow.start}

	main = append(main, m.metaView())
	if reportLines > 0 {
		main = append(main, m.reportLines(reportLines)...)
	}
	m.hit.filter = bodyTop + len(main)
	main = append(main, m.filterView())

	if m.showsTop() {
		formatBody, formatWindow := m.formatsPanel(formatRows)
		formatTop := len(main)
		main = append(main, boxLines(m.videoPanelTitle(), formatBody, m.contentWidth(), m.area == areaFormats)...)
		m.hit.formats = panelHit{top: bodyTop + formatTop + 2, rows: formatWindow.rows, start: formatWindow.start}
	} else {
		m.hit.formats = panelHit{}
	}
	if muxedRows > 0 {
		muxedBody, muxedWindow := m.muxedPanel(muxedRows)
		muxedTop := len(main)
		main = append(main, boxLines(m.panelTitle("muxed", len(m.muxed)), muxedBody, m.contentWidth(), m.area == areaMuxed)...)
		m.hit.muxed = panelHit{top: bodyTop + muxedTop + 2, rows: muxedWindow.rows, start: muxedWindow.start}
	} else {
		m.hit.muxed = panelHit{}
	}

	if slots {
		main = append(main, m.slotsView())
	}
	for len(main) < bodyH {
		main = append(main, "")
	}
	if len(main) > bodyH {
		main = main[:bodyH]
	}

	if sideLog {
		entries := m.logEntries(max(0, bodyH-2), max(8, logWidth-2))
		for len(entries) < bodyH-2 {
			entries = append(entries, "")
		}
		box := boxLines("log", entries, logWidth, false)
		lines = append(lines, joinColumns(main, m.contentWidth(), box, bodyH)...)
	} else {
		lines = append(lines, main...)
	}
	lines = append(lines, m.footerView())

	if len(lines) > H && H > 0 {
		lines = lines[:H]
	}
	return strings.Join(lines, "\n")
}

// reportLineBudget decides how many availability lines fit on this height.
func (m *Model) reportLineBudget(job *format.Job) int {
	if job == nil || !job.Analyzed() {
		return 0
	}
	total := len(format.AvailabilityReport(job.Formats, m.mergeFormat))
	if m.height < 24 {
		return 0
	}
	if m.height < 30 {
		return min(total, 1)
	}
	if m.height < 38 {
		return min(total, 2)
	}
	return min(total, 4)
}

// ---------------------------------------------------------------------------
// frame pieces
// ---------------------------------------------------------------------------

func (m *Model) headerView() string {
	left := " ytd — yt-dlp queue"
	right := "out " + m.outdir + " "
	inner := m.width
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)
	if lw+rw+2 > inner {
		right = ""
		rw = 0
	}
	gap := max(1, inner-lw-rw)
	line := left + strings.Repeat(" ", gap) + right
	line = padRight(truncate(line, inner), inner)
	return headerStyle.Render(line)
}

func (m *Model) urlBox() []string {
	return boxLines("url", []string{m.urlView()}, m.width, m.area == areaURL)
}

func (m *Model) urlView() string {
	var line string
	switch {
	case m.area == areaURL:
		line = m.urlInput.View()
	case m.urlInput.Value() == "":
		line = "› paste a URL and press Enter"
	default:
		line = "› " + m.urlInput.Value()
	}
	line = truncateANSI(line, max(8, m.width-2))
	if m.area == areaURL {
		return padRight(line, max(8, m.width-2))
	}
	return dimStyle.Render(line)
}

// panelTitle is the label drawn in a panel's top border.
func (m *Model) panelTitle(name string, count int) string {
	if count <= 0 {
		return name
	}
	return fmt.Sprintf("%s · %d", name, count)
}

// boxLines draws a rounded border around body with the title in the top edge.
// Every returned line is exactly width columns, which keeps the columns aligned
// and the mouse hit-test math exact.
func boxLines(title string, body []string, width int, focused bool) []string {
	width = max(6, width)
	inner := width - 2
	style := borderIdle
	if focused {
		style = borderFocus
	}
	title = truncate(title, max(1, width-6))
	prefix := "╭─ " + title + " "
	fill := max(0, width-ansi.StringWidth(prefix)-1)
	out := make([]string, 0, len(body)+2)
	out = append(out, style.Render(prefix+strings.Repeat("─", fill)+"╮"))
	for _, line := range body {
		out = append(out, style.Render("│")+padRight(truncateANSI(line, inner), inner)+style.Render("│"))
	}
	out = append(out, style.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return out
}

// joinColumns places the log box to the right of the main column, line by line.
func joinColumns(left []string, leftWidth int, right []string, height int) []string {
	out := make([]string, 0, height)
	for index := 0; index < height; index++ {
		line := ""
		if index < len(left) {
			line = left[index]
		}
		next := ""
		if index < len(right) {
			next = right[index]
		}
		out = append(out, padRight(line, leftWidth)+next)
	}
	return out
}

// ---------------------------------------------------------------------------
// queue
// ---------------------------------------------------------------------------

type queueColumn struct {
	key   string
	title string
	width int
}

// queueColumns picks the columns that fit the main column, giving the leftover
// room to the title.
func queueColumns(width int) []queueColumn {
	index := queueColumn{"index", "#", 3}
	title := queueColumn{"title", "título", 0}
	quality := queueColumn{"quality", "calidad", 13}
	audio := queueColumn{"audio", "audio", 13}
	status := queueColumn{"status", "estado", 15}
	size := queueColumn{"size", "tamaño", 9}

	cols := []queueColumn{index, title, quality, audio, status, size}
	switch {
	case width < 62:
		cols = []queueColumn{index, title, status, size}
	case width < 80:
		quality.width, audio.width = 11, 11
	}
	fixed := len(cols) - 1
	for _, column := range cols {
		if column.key != "title" {
			fixed += column.width
		}
	}
	for position := range cols {
		if cols[position].key == "title" {
			cols[position].width = clamp(width-fixed, 10, 90)
		}
	}
	return cols
}

func queueHeader(cols []queueColumn) string {
	widths := make([]int, len(cols))
	cells := make([]string, len(cols))
	for index, column := range cols {
		widths[index] = column.width
		cells[index] = column.title
	}
	return joinCells(widths, cells)
}

// queuePanel renders the column header plus up to rows rows, scrolled so the
// focused job stays visible.
func (m *Model) queuePanel(rows int) ([]string, panelHit) {
	cols := queueColumns(m.panelInner())
	body := []string{dimStyle.Render(queueHeader(cols))}
	window := panelHit{}
	if rows <= 0 {
		return body, window
	}
	window.rows = rows
	start := 0
	if m.focus >= rows {
		start = m.focus - rows + 1
	}
	window.start = start
	for offset := 0; offset < rows; offset++ {
		index := start + offset
		if index >= len(m.jobs) {
			if offset == 0 && len(m.jobs) == 0 {
				body = append(body, dimStyle.Render("  (empty) paste a URL above"))
			} else {
				body = append(body, "")
			}
			continue
		}
		body = append(body, m.queueRow(cols, m.jobs[index], index, index == m.focus))
	}
	return body, window
}

func (m *Model) queueRow(cols []queueColumn, job format.Job, index int, focused bool) string {
	cells := make([]string, len(cols))
	widths := make([]int, len(cols))
	for position, column := range cols {
		widths[position] = column.width
		switch column.key {
		case "index":
			cells[position] = fmt.Sprintf("%d", index+1)
		case "title":
			cells[position] = job.Label(0)
		case "quality":
			cells[position] = job.QualityLabel()
		case "audio":
			cells[position] = job.AudioLabel()
		case "status":
			cells[position] = statusCell(job, column.width)
		case "size":
			cells[position] = job.SizeLabel()
		}
	}
	raw := joinCells(widths, cells)
	if focused {
		style := cursorOffStyle
		if m.area == areaQueue {
			style = cursorOnStyle
		}
		return style.Render(padRight(raw, m.panelInner()))
	}
	parts := make([]string, len(cols))
	for position, column := range cols {
		text := padRight(truncate(cells[position], widths[position]), widths[position])
		switch column.key {
		case "index":
			text = dimStyle.Render(text)
		case "status":
			text = statusStyles[job.Status].Render(text)
		}
		parts[position] = text
	}
	return truncateANSI(strings.Join(parts, " "), m.panelInner())
}

// statusCell is the estado column: a progress bar while the row downloads and
// the plain status label otherwise.
func statusCell(job format.Job, width int) string {
	if job.Status != format.Downloading {
		return format.StatusLabel(job.Status, job.Percent, job.Error)
	}
	percent := clamp(int(job.Percent+0.5), 0, 100)
	barWidth := max(4, width-6)
	filled := barWidth * percent / 100
	return strings.Repeat("█", filled) +
		strings.Repeat("░", barWidth-filled) +
		fmt.Sprintf(" %3d%%", percent)
}

// ---------------------------------------------------------------------------
// meta, report, filter
// ---------------------------------------------------------------------------

func (m *Model) metaView() string {
	job := m.focusedJob()
	if job == nil {
		return dimStyle.Render(" no video loaded")
	}
	width := m.contentWidth()
	title := titleStyle.Render(job.Label(max(20, width/2)))
	if !job.Analyzed() {
		return " " + title
	}
	bits := []string{title}
	if job.Uploader != "" {
		bits = append(bits, job.Uploader)
	}
	if job.Duration > 0 {
		bits = append(bits, format.HumanDuration(job.Duration))
	}
	bits = append(bits, fmt.Sprintf("%d formats", len(job.Formats)))
	if job.Status == format.Downloading {
		bits = append(bits, fmt.Sprintf("%.0f%%", job.Percent))
		if job.Speed != "" {
			bits = append(bits, job.Speed)
		}
		if job.ETA != "" {
			bits = append(bits, "eta "+job.ETA)
		}
	}
	bits = append(bits, "saves to "+m.outdir)
	return truncateANSI(" "+strings.Join(bits, dimStyle.Render("  ·  ")), width)
}

func (m *Model) reportLines(limit int) []string {
	job := m.focusedJob()
	if job == nil {
		return nil
	}
	report := format.AvailabilityReport(job.Formats, m.mergeFormat)
	lines := make([]string, 0, min(len(report), limit))
	for _, line := range report {
		if len(lines) >= limit {
			break
		}
		width := m.contentWidth()
		text := truncateANSI("  "+line.Text, width)
		switch line.Kind {
		case format.LineWarning:
			text = warnStyle.Render(text)
		case format.LineAudio:
			text = audioStyle.Render(text)
		case format.LineInfo:
			text = dimStyle.Render(text)
		}
		lines = append(lines, text)
	}
	return lines
}

func (m *Model) filterView() string {
	var left string
	if m.area == areaFilter {
		left = m.filterInput.View()
	} else if value := m.filterInput.Value(); value != "" {
		left = "/ " + value
	} else {
		left = dimStyle.Render("/ filter: 1080, av1, opus…")
	}
	tags := fmt.Sprintf("ext:%s  sized:%s  hls:%s  compat:%s",
		containerOptions[m.contIdx],
		onOff(m.onlySize), onOff(!m.hideHLS), onOff(m.compatOnly))
	return truncateANSI(" "+left+"    "+dimStyle.Render(tags), m.contentWidth())
}

func onOff(on bool) string {
	if on {
		return accentStyle.Render("on")
	}
	return dimStyle.Render("off")
}

// ---------------------------------------------------------------------------
// format explorer
// ---------------------------------------------------------------------------

// colSpec is one column of a formats table.
type colSpec struct {
	key   string
	title string
	width int
}

// columnBuilder lays out a table's columns for a given inner width.
type columnBuilder func(width int) []colSpec

func colsWidth(cols []colSpec) int {
	total := 0
	for _, c := range cols {
		total += c.width
	}
	return total + max(0, len(cols)-1)
}

// fitColumns keeps the columns that fit, dropping the least important ones from
// the end (the note goes first) and handing any leftover room to the note.
func fitColumns(width int, cols []colSpec) []colSpec {
	out := append([]colSpec(nil), cols...)
	for len(out) > 3 && colsWidth(out) > width {
		// Drop the last fixed column, keeping the note at the tail.
		out = append(out[:len(out)-2], out[len(out)-1])
	}
	if last := len(out) - 1; out[last].key == "note" {
		fixed := colsWidth(out) - out[last].width
		if leftover := width - fixed - 1; leftover >= 10 {
			out[last].width = leftover
		} else {
			out = out[:last]
		}
	}
	return out
}

// videoColumns is the video stream table: no id, ext, acodec or hdr.
func videoColumns(width int) []colSpec {
	return fitColumns(width, []colSpec{
		{"sel", "sel", 3},
		{"res", "res", 8},
		{"vcodec", "vcodec", 9},
		{"fps", "fps", 4},
		{"br", "br", 7},
		{"size", "size", 8},
		{"note", "note", 0},
	})
}

// audioColumns is the audio stream table shown after a video is picked.
func audioColumns(width int) []colSpec {
	return fitColumns(width, []colSpec{
		{"sel", "sel", 3},
		{"acodec", "acodec", 9},
		{"br", "br", 7},
		{"size", "size", 8},
		{"note", "note", 0},
	})
}

// muxedColumns is the progressive table: both codecs, still no id or ext.
func muxedColumns(width int) []colSpec {
	return fitColumns(width, []colSpec{
		{"sel", "sel", 3},
		{"res", "res", 8},
		{"vcodec", "vcodec", 9},
		{"acodec", "acodec", 9},
		{"fps", "fps", 4},
		{"br", "br", 7},
		{"size", "size", 8},
		{"note", "note", 0},
	})
}

// videoPanelTitle labels the top table with what it currently lists.
func (m *Model) videoPanelTitle() string {
	name := "videos"
	if m.stage == stageAudio {
		name = "audio"
		if job := m.focusedJob(); job != nil && job.VideoID != "" {
			name += " for " + job.VideoID
		}
	}
	if len(m.visible) == 0 {
		return name
	}
	return fmt.Sprintf("%s · %d", name, len(m.visible))
}

// formatsPanel renders the top table: the video streams, or the audio streams of
// the video already chosen.
func (m *Model) formatsPanel(rows int) ([]string, panelHit) {
	builder := columnBuilder(videoColumns)
	if m.stage == stageAudio {
		builder = audioColumns
	}
	return m.formatTable(builder, m.visible, m.fmtCursor, rows, m.area == areaFormats,
		"(no formats match — adjust the filter)")
}

// muxedPanel renders the progressive (video+audio) formats table.
func (m *Model) muxedPanel(rows int) ([]string, panelHit) {
	return m.formatTable(muxedColumns, m.muxed, m.muxedCursor, rows, m.area == areaMuxed,
		"(no muxed formats)")
}

// formatTable renders a column header plus up to rows rows, scrolled so the
// cursor stays visible.
func (m *Model) formatTable(builder columnBuilder, entries []format.Fmt, cursor, rows int, active bool, empty string) ([]string, panelHit) {
	cols := builder(m.panelInner())
	body := []string{dimStyle.Render(formatHeader(cols))}
	window := panelHit{}
	if rows <= 0 {
		return body, window
	}
	window.rows = rows
	start := 0
	if cursor >= rows {
		start = cursor - rows + 1
	}
	window.start = start
	for offset := 0; offset < rows; offset++ {
		index := start + offset
		if index >= len(entries) {
			if offset == 0 && len(entries) == 0 {
				body = append(body, dimStyle.Render("  "+empty))
			} else {
				body = append(body, "")
			}
			continue
		}
		body = append(body, m.formatRow(cols, entries[index], index == cursor, active))
	}
	return body, window
}

func formatHeader(cols []colSpec) string {
	cells := make([]string, len(cols))
	widths := make([]int, len(cols))
	for i, c := range cols {
		cells[i] = c.title
		widths[i] = c.width
	}
	return joinCells(widths, cells)
}

func (m *Model) formatRow(cols []colSpec, candidate format.Fmt, cursor, active bool) string {
	values := m.formatValues(candidate)
	raw := make([]string, len(cols))
	for i, c := range cols {
		raw[i] = padRight(truncate(values[c.key], c.width), c.width)
	}
	plain := strings.Join(raw, " ")
	if cursor {
		style := cursorOffStyle
		if active {
			style = cursorOnStyle
		}
		return style.Render(padRight(plain, m.panelInner()))
	}
	parts := make([]string, len(cols))
	for i, c := range cols {
		text := raw[i]
		if style, ok := m.formatCellStyle(c.key, candidate); ok {
			text = style.Render(text)
		}
		parts[i] = text
	}
	return truncateANSI(strings.Join(parts, " "), m.panelInner())
}

func (m *Model) formatValues(candidate format.Fmt) map[string]string {
	mark := ""
	job := m.focusedJob()
	if job != nil {
		if candidate.ID == job.VideoID {
			mark = "V"
		}
		if candidate.ID == job.AudioID {
			if mark == "" {
				mark = "A"
			} else {
				mark = "V+A"
			}
		}
	}
	note := candidate.Note
	if candidate.Language != "" {
		note = strings.TrimSpace(note + " [" + candidate.Language + "]")
	}
	if problem := format.CompatibilityNote(candidate, m.mergeFormat); problem != "" {
		note = strings.TrimSpace(note + " ⚠ " + problem)
	}
	return map[string]string{
		"sel":    mark,
		"res":    candidate.Resolution(),
		"vcodec": firstNonEmpty(candidate.VCodecName(), "—"),
		"acodec": firstNonEmpty(candidate.ACodecName(), "—"),
		"fps":    fpsLabel(candidate),
		"br":     format.HumanBitrate(candidate.TBR),
		"size":   format.HumanSize(candidate.Size),
		"note":   note,
	}
}

func (m *Model) formatCellStyle(key string, candidate format.Fmt) (lipgloss.Style, bool) {
	switch key {
	case "sel":
		return selectionMark, true
	case "note":
		if problem := format.CompatibilityNote(candidate, m.mergeFormat); problem != "" {
			return warnStyle, true
		}
		return dimStyle, true
	}
	return lipgloss.Style{}, false
}

func fpsLabel(candidate format.Fmt) string {
	if candidate.FPS > 1 {
		return fmt.Sprintf("%d", int(candidate.FPS))
	}
	return "—"
}

// ---------------------------------------------------------------------------
// slots, log and footer
// ---------------------------------------------------------------------------

func (m *Model) slotsView() string {
	job := m.focusedJob()
	if job == nil {
		return slotStyle.Render(truncate("no row selected", max(10, m.contentWidth()-2)))
	}
	describe := func(candidate *format.Fmt) string {
		if candidate == nil {
			return "—"
		}
		codec := firstNonEmpty(candidate.VCodecName(), candidate.ACodecName(), "?")
		return fmt.Sprintf("%s (%s, %s, %s)", candidate.ID, codec, candidate.Resolution(), format.HumanSize(candidate.Size))
	}
	selector := job.Selector()
	if selector == "" {
		selector = "auto (best)"
	}
	text := fmt.Sprintf(" video: %s    audio: %s    -f %s",
		describe(job.Video()), describe(job.Audio()), selector)
	if video := job.Video(); video != nil && video.Kind == format.Video && job.AudioID == "" {
		text += "  ⚠ no audio: file will be silent"
	}
	return slotStyle.Render(truncate(text, max(10, m.contentWidth()-2)))
}

// logEntries renders the last limit log lines at the given width.
func (m *Model) logEntries(limit, width int) []string {
	if limit <= 0 {
		return nil
	}
	entries := m.logLines
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		text := truncate(entry.text, max(8, width))
		switch entry.level {
		case "warn":
			text = warnStyle.Render(text)
		case "error":
			text = errorStyle.Render(text)
		case "ok":
			text = okStyle.Render(text)
		default:
			text = dimStyle.Render(text)
		}
		out = append(out, text)
	}
	return out
}

func (m *Model) footerView() string {
	tail := "q quit"
	if m.notice != "" {
		room := max(5, m.width-lipgloss.Width(tail)-3)
		notice := warnStyle.Render(" " + truncate(m.notice, room))
		return truncateANSI(notice+"  "+footerStyle.Render(tail), m.width)
	}
	segments := []string{
		"tab pane", "↑↓ move", "enter pick", "← back", "d download", "e edit",
		"s start", "x cancel", "f filter", "l log",
	}
	room := m.width - lipgloss.Width(tail) - 3
	left := ""
	for _, segment := range segments {
		candidate := segment
		if left != "" {
			candidate = left + " · " + segment
		}
		if lipgloss.Width(candidate) > room {
			break
		}
		left = candidate
	}
	line := tail
	if left != "" {
		line = left + " · " + tail
	}
	line = padRight(truncate(line, m.width), m.width)
	return footerStyle.Render(line)
}

// ---------------------------------------------------------------------------
// small rendering helpers
// ---------------------------------------------------------------------------

func truncateANSI(text string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(text, width, "…")
}

func joinCells(widths []int, cells []string) string {
	parts := make([]string, len(cells))
	for index, cell := range cells {
		limit := widths[index]
		parts[index] = padRight(truncate(cell, limit), limit)
	}
	return strings.TrimRight(strings.Join(parts, " "), " ")
}

func padRight(text string, width int) string {
	gap := width - ansi.StringWidth(text)
	if gap <= 0 {
		return text
	}
	return text + strings.Repeat(" ", gap)
}

func lipglossBox(content string, width int) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("6")).
		Width(width).
		Padding(1, 2).
		Render(content)
}

func placeCenter(content string, width, height int) string {
	boxWidth := lipgloss.Width(content)
	boxHeight := lipgloss.Height(content)
	left := max(0, (width-boxWidth)/2)
	top := max(0, (height-boxHeight)/2)
	lines := strings.Split(content, "\n")
	prefix := strings.Repeat(" ", left)
	padded := make([]string, 0, len(lines)+top)
	for index := 0; index < top; index++ {
		padded = append(padded, "")
	}
	for _, line := range lines {
		padded = append(padded, prefix+line)
	}
	return strings.Join(padded, "\n")
}
