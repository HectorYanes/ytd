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
)

const (
	logLinesShown = 6
	queueMaxRows  = 6
)

// kindStyle colours a stream by its kind, mirroring the Textual palette.
func kindStyle(kind format.Kind) lipgloss.Style {
	switch kind {
	case format.Muxed:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
	case format.Video:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // cyan
	case format.Audio:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	case format.Image:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")) // bright black
	}
	return lipgloss.NewStyle()
}

// screen accumulates rendered lines so block renderers can report the exact
// terminal rows they occupy to the mouse hit-test map.
type screen struct {
	lines []string
}

func (s *screen) add(text string) {
	if text == "" {
		return
	}
	s.lines = append(s.lines, strings.Split(text, "\n")...)
}

func (s *screen) blank() { s.lines = append(s.lines, "") }

func (s *screen) count() int { return len(s.lines) }

// View renders the whole screen: a queue strip, the focused video's metadata,
// and the format explorer that owns the remaining height.
func (m *Model) View() string {
	if m.editor != nil {
		return m.editorView()
	}
	if m.width <= 0 {
		return ""
	}
	H := m.height

	job := m.focusedJob()
	progressLines := 0
	if job != nil && job.Status == format.Downloading {
		progressLines = 1
	}
	logLines := 0
	if m.showLog {
		logLines = min(len(m.logLines), logLinesShown) + 1
	}
	reportLines := m.reportLineBudget(job)

	// Fixed chrome: header, url, queue title, queue column header, meta,
	// filters, formats title, slots, footer.
	const fixed = 9
	const spacer = 1
	urlGap := 0
	if H >= 26 {
		urlGap = 1
	}
	budget := H - fixed - reportLines - logLines - progressLines - spacer - urlGap

	queueRows := 0
	if len(m.jobs) == 0 {
		queueRows = 1 // placeholder line
	} else {
		queueRows = min(len(m.jobs), queueMaxRows)
		if H < 34 {
			queueRows = min(queueRows, 4)
		}
		if H < 28 {
			queueRows = min(queueRows, 2)
		}
	}
	if budget < queueRows+2 {
		queueRows = max(0, budget-2)
	}
	formatRows := budget - queueRows
	if formatRows < 2 {
		need := 2 - formatRows
		if queueRows >= need {
			queueRows -= need
			formatRows = 2
		} else {
			formatRows = max(0, formatRows+queueRows)
			queueRows = 0
		}
	}
	if formatRows < 0 {
		formatRows = 0
	}

	s := &screen{}
	s.add(m.headerView())

	urlTop := s.count()
	s.add(m.urlView())
	if urlGap > 0 {
		s.blank()
	}

	queueTitleTop := s.count()
	queueBlock, queueHit := m.queueBlock(queueRows)
	s.add(queueBlock)
	queueHit.top += queueTitleTop
	m.hit.queue = queueHit

	s.add(m.metaView())
	if reportLines > 0 {
		s.add(m.reportView(reportLines))
	}
	filterTop := s.count()
	s.add(m.filterView())
	m.hit.filter = filterTop
	s.blank()

	formatTitleTop := s.count()
	formatBlock, formatHit := m.formatsBlock(formatRows)
	s.add(formatBlock)
	formatHit.top += formatTitleTop + 1
	m.hit.formats = formatHit

	s.add(m.slotsView())
	if progressLines > 0 {
		s.add(m.progressView())
	}
	if m.showLog {
		s.add(m.logView())
	}
	s.add(m.footerView())

	// Never let the frame exceed the terminal: the footer must stay visible.
	if len(s.lines) > H && H > 0 {
		s.lines = s.lines[:H]
	}
	m.hit.url = urlTop
	return strings.Join(s.lines, "\n")
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
	line = truncateANSI(line, m.width)
	if m.area == areaURL {
		return padRight(line, m.width)
	}
	return dimStyle.Render(line)
}

// queueBlock renders the compact queue strip: a title badge and up to rows
// rows, scrolled so the focused job stays visible.
func (m *Model) queueBlock(rows int) (string, panelHit) {
	title := m.sectionTitle("queue", len(m.jobs))
	lines := []string{title}
	hit := panelHit{top: 1, start: 0}
	if rows <= 0 {
		return strings.Join(lines, "\n"), hit
	}
	start := 0
	if m.focus >= rows {
		start = m.focus - rows + 1
	}
	hit.start = start
	hit.rows = rows

	widths := queueWidths(m.width)
	lines = append(lines, dimStyle.Render(queueHeader(widths)))
	for i := 0; i < rows; i++ {
		index := start + i
		if index >= len(m.jobs) {
			if i == 0 && len(m.jobs) == 0 {
				lines = append(lines, dimStyle.Render("  (empty) paste a URL above"))
			} else {
				lines = append(lines, "")
			}
			continue
		}
		lines = append(lines, m.queueRow(m.jobs[index], index, index == m.focus))
	}
	return strings.Join(lines, "\n"), hit
}

func queueWidths(width int) []int {
	size, status, audio, quality := 9, 14, 15, 15
	title := clamp(width-(3+quality+audio+status+size)-5, 12, 90)
	return []int{3, title, quality, audio, status, size}
}

func queueHeader(widths []int) string {
	return joinCells(widths, []string{"#", "título", "calidad", "audio", "estado", "tamaño"})
}

func (m *Model) queueRow(job format.Job, index int, focused bool) string {
	widths := queueWidths(m.width)
	cells := []string{
		fmt.Sprintf("%d", index+1),
		job.Label(0),
		job.QualityLabel(),
		job.AudioLabel(),
		format.StatusLabel(job.Status, job.Percent, job.Error),
		job.SizeLabel(),
	}
	raw := joinCells(widths, cells)
	if focused {
		style := cursorOffStyle
		if m.area == areaQueue {
			style = cursorOnStyle
		}
		return style.Render(padRight(raw, m.width))
	}
	parts := make([]string, len(cells))
	for i, cell := range cells {
		text := padRight(truncate(cell, widths[i]), widths[i])
		switch i {
		case 0:
			text = dimStyle.Render(text)
		case 4:
			text = statusStyles[job.Status].Render(text)
		}
		parts[i] = text
	}
	return truncateANSI(strings.Join(parts, " "), m.width)
}

func (m *Model) sectionTitle(name string, count int) string {
	badge := titleStyle.Render(" " + name + " ")
	rest := ""
	if count > 0 {
		rest = dimStyle.Render(fmt.Sprintf("  %d", count))
	}
	return badge + rest
}

func (m *Model) metaView() string {
	job := m.focusedJob()
	if job == nil {
		return dimStyle.Render(" no video loaded")
	}
	title := titleStyle.Render(job.Label(max(20, m.width/2)))
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
	bits = append(bits, "saves to "+m.outdir)
	return truncateANSI(" "+strings.Join(bits, dimStyle.Render("  ·  ")), m.width)
}

func (m *Model) reportView(limit int) string {
	job := m.focusedJob()
	if job == nil {
		return ""
	}
	report := format.AvailabilityReport(job.Formats, m.mergeFormat)
	if len(report) == 0 {
		return ""
	}
	lines := make([]string, 0, min(len(report), limit))
	for _, line := range report {
		if len(lines) >= limit {
			break
		}
		text := "  " + line.Text
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
	return strings.Join(lines, "\n")
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
	tags := fmt.Sprintf("kind:%s  ext:%s  sized:%s  hls:%s  compat:%s",
		kindOptions[m.kindIdx], containerOptions[m.contIdx],
		onOff(m.onlySize), onOff(!m.hideHLS), onOff(m.compatOnly))
	return truncateANSI(" "+left+"    "+dimStyle.Render(tags), m.width)
}

func onOff(on bool) string {
	if on {
		return accentStyle.Render("on")
	}
	return dimStyle.Render("off")
}

// colSpec is one column of the format explorer.
type colSpec struct {
	key   string
	title string
	width int
}

func colsWidth(cols []colSpec) int {
	total := 0
	for _, c := range cols {
		total += c.width
	}
	return total + max(0, len(cols)-1)
}

// formatColumns picks the widest column set that fits, dropping optional
// columns on narrow terminals and giving the rest to the free-form note.
func formatColumns(width int) []colSpec {
	sel := colSpec{"sel", "sel", 3}
	id := colSpec{"id", "id", 6}
	kind := colSpec{"kind", "kind", 6}
	res := colSpec{"res", "res", 8}
	vcodec := colSpec{"vcodec", "vcodec", 9}
	acodec := colSpec{"acodec", "acodec", 9}
	hdr := colSpec{"hdr", "hdr", 6}
	fps := colSpec{"fps", "fps", 4}
	br := colSpec{"br", "br", 8}
	size := colSpec{"size", "size", 9}
	ext := colSpec{"ext", "ext", 5}
	src := colSpec{"src", "src", 5}
	note := colSpec{"note", "note", 0}

	if width >= 92 {
		cols := []colSpec{sel, id, kind, res, vcodec, acodec, hdr, fps, br, size, ext, src}
		if leftover := width - colsWidth(cols) - 1; leftover >= 10 {
			note.width = leftover
			cols = append(cols, note)
		}
		return cols
	}
	if width >= 84 {
		return []colSpec{sel, id, kind, res, vcodec, acodec, hdr, fps, br, size, ext}
	}
	if width >= 72 {
		return []colSpec{sel, id, kind, res, vcodec, acodec, br, size, ext}
	}
	return []colSpec{sel, id, kind, res, vcodec, acodec, size}
}

func (m *Model) formatsBlock(rows int) (string, panelHit) {
	cols := formatColumns(m.width)
	lines := []string{dimStyle.Render(formatHeader(cols))}
	hit := panelHit{top: 1, start: 0}
	if rows <= 0 {
		return strings.Join(lines, "\n"), hit
	}
	hit.rows = rows
	start := 0
	if m.fmtCursor >= rows {
		start = m.fmtCursor - rows + 1
	}
	hit.start = start
	for i := 0; i < rows; i++ {
		index := start + i
		if index >= len(m.visible) {
			if i == 0 && len(m.visible) == 0 {
				lines = append(lines, dimStyle.Render("  (no formats match — adjust the filter)"))
			} else {
				lines = append(lines, "")
			}
			continue
		}
		lines = append(lines, m.formatRow(cols, m.visible[index], index == m.fmtCursor))
	}
	return strings.Join(lines, "\n"), hit
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

func (m *Model) formatRow(cols []colSpec, candidate format.Fmt, cursor bool) string {
	values := m.formatValues(candidate)
	raw := make([]string, len(cols))
	for i, c := range cols {
		raw[i] = padRight(truncate(values[c.key], c.width), c.width)
	}
	plain := strings.Join(raw, " ")
	if cursor {
		style := cursorOffStyle
		if m.area == areaFormats {
			style = cursorOnStyle
		}
		return style.Render(padRight(plain, m.width))
	}
	parts := make([]string, len(cols))
	for i, c := range cols {
		text := raw[i]
		if style, ok := m.formatCellStyle(c.key, candidate); ok {
			text = style.Render(text)
		}
		parts[i] = text
	}
	return truncateANSI(strings.Join(parts, " "), m.width)
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
	source := candidate.Protocol
	if candidate.IsHLS() {
		source = "hls"
	}
	return map[string]string{
		"sel":    mark,
		"id":     candidate.ID,
		"kind":   string(candidate.Kind),
		"res":    candidate.Resolution(),
		"vcodec": firstNonEmpty(candidate.VCodecName(), "—"),
		"acodec": firstNonEmpty(candidate.ACodecName(), "—"),
		"hdr":    firstNonEmpty(candidate.HDRLabel(), "—"),
		"fps":    fpsLabel(candidate),
		"br":     format.HumanBitrate(candidate.TBR),
		"size":   format.HumanSize(candidate.Size),
		"ext":    candidate.Ext,
		"src":    firstNonEmpty(source, "—"),
		"note":   note,
	}
}

func (m *Model) formatCellStyle(key string, candidate format.Fmt) (lipgloss.Style, bool) {
	switch key {
	case "sel":
		return selectionMark, true
	case "id":
		return kindStyle(candidate.Kind).Bold(true), true
	case "kind":
		return kindStyle(candidate.Kind), true
	case "hdr":
		if candidate.HDRLabel() != "" {
			return hdrStyle, true
		}
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

func (m *Model) slotsView() string {
	job := m.focusedJob()
	if job == nil {
		return slotStyle.Render(truncate("no row selected", max(10, m.width-2)))
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
	return slotStyle.Render(truncate(text, max(10, m.width-2)))
}

func (m *Model) progressView() string {
	job := m.focusedJob()
	if job == nil || job.Status != format.Downloading {
		return ""
	}
	percent := job.Percent
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	barWidth := clamp(m.width-40, 10, 60)
	filled := int(percent / 100 * float64(barWidth))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	text := fmt.Sprintf(" download %3.0f%% [%s]", percent, bar)
	if job.Speed != "" {
		text += "  " + job.Speed
	}
	if job.ETA != "" {
		text += "  eta " + job.ETA
	}
	return progressStyle.Render(truncate(text, m.width))
}

func (m *Model) logView() string {
	lines := m.logLines
	if len(lines) > logLinesShown {
		lines = lines[len(lines)-logLinesShown:]
	}
	rendered := make([]string, 0, len(lines)+1)
	rendered = append(rendered, dimStyle.Render(" log"))
	for _, entry := range lines {
		text := truncate(entry.text, max(10, m.width-4))
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
		rendered = append(rendered, "  "+text)
	}
	return strings.Join(rendered, "\n")
}

func (m *Model) footerView() string {
	tail := "q quit"
	if m.notice != "" {
		room := max(5, m.width-lipgloss.Width(tail)-3)
		notice := warnStyle.Render(" " + truncate(m.notice, room))
		return truncateANSI(notice+"  "+footerStyle.Render(tail), m.width)
	}
	segments := []string{
		"tab pane", "↑↓ move", "v video", "a audio", "d download", "e edit",
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
