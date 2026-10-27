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

const (
	maxReportLines = 4
	logLinesShown  = 5
)

// View renders the whole screen.
func (m *Model) View() string {
	if m.editor != nil {
		return m.editorView()
	}
	if m.width <= 0 {
		return ""
	}

	queueRows := 6
	formatRows := 8
	logHeight := 0
	if m.showLog {
		logHeight = logLinesShown + 1
	}
	available := m.height - (9 + maxReportLines + logHeight)
	if available > 6 {
		queueRows = clamp(available/2, 3, 14)
		formatRows = clamp(available-queueRows, 3, 20)
	}

	sections := []string{
		m.headerView(),
		m.urlView(),
		m.queueView(queueRows),
		m.reportView(),
		m.filterView(),
		m.formatsView(formatRows),
		m.slotsView(),
	}
	if m.showLog {
		sections = append(sections, m.logView())
	}
	sections = append(sections, m.footerView())
	return strings.Join(sections, "\n")
}

func (m *Model) headerView() string {
	left := titleStyle.Render("ytd — yt-dlp queue")
	right := dimStyle.Render("out " + m.outdir)
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) urlView() string {
	view := m.urlInput.View()
	if m.area != areaURL {
		view = dimStyle.Render("› " + truncate(m.urlInput.Value(), max(10, m.width-6)))
	}
	return truncateANSI(view, m.width)
}

func (m *Model) queueView(limit int) string {
	widths := m.queueWidths()
	lines := []string{dimStyle.Render(joinCells(widths, []string{"#", "título", "calidad", "audio", "estado", "tamaño"}))}
	if len(m.jobs) == 0 {
		return strings.Join(append(lines, dimStyle.Render("  (empty — paste a URL above)")), "\n")
	}
	start := 0
	if m.focus >= limit {
		start = m.focus - limit + 1
	}
	for index := start; index < len(m.jobs) && index < start+limit; index++ {
		job := m.jobs[index]
		cells := []string{
			fmt.Sprintf("%d", index+1),
			job.Label(0),
			job.QualityLabel(),
			job.AudioLabel(),
			format.StatusLabel(job.Status, job.Percent, job.Error),
			job.SizeLabel(),
		}
		row := joinCells(widths, cells)
		if index == m.focus {
			row = cursorStyle.Render(truncateANSI(row, m.width))
		}
		lines = append(lines, row)
	}
	return strings.Join(lines, "\n")
}

func (m *Model) queueWidths() []int {
	title := clamp(m.width-60, 18, 60)
	return []int{3, title, 10, 10, 18, 10}
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

func (m *Model) reportView() string {
	job := m.focusedJob()
	var lines []string
	switch {
	case job == nil:
		lines = []string{dimStyle.Render("Paste a URL to start a queue.")}
	case !job.Analyzed():
		lines = []string{dimStyle.Render("Formats not fetched. Press `e` to analyze this row.")}
	default:
		for _, line := range format.AvailabilityReport(job.Formats, m.mergeFormat) {
			styled := line.Text
			switch line.Kind {
			case format.LineWarning:
				styled = warnStyle.Render(line.Text)
			case format.LineAudio:
				styled = audioStyle.Render(line.Text)
			}
			lines = append(lines, styled)
		}
		if len(lines) == 0 {
			lines = []string{dimStyle.Render("No formats.")}
		}
	}
	if len(lines) > maxReportLines {
		lines = append(lines[:maxReportLines-1], dimStyle.Render(fmt.Sprintf("… %d more", len(lines)-maxReportLines+1)))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) filterView() string {
	return truncateANSI(m.filterInput.View(), m.width) + "  " +
		dimStyle.Render(fmt.Sprintf("kind:%s ext:%s sized:%v hls:%v compat:%v",
			kindOptions[m.kindIdx], containerOptions[m.contIdx], !m.onlySize, !m.hideHLS, m.compatOnly))
}

func (m *Model) formatsView(limit int) string {
	widths := []int{4, 7, 7, 9, 10, 10, 6, 5, 9, 10, 6, 8, 30}
	header := []string{"sel", "id", "kind", "res", "vcodec", "acodec", "hdr", "fps", "br", "size", "ext", "src", "note"}
	lines := []string{dimStyle.Render(joinCells(widths, header))}
	if len(m.visible) == 0 {
		lines = append(lines, dimStyle.Render("  (no formats match)"))
		return strings.Join(lines, "\n")
	}
	job := m.focusedJob()
	start := 0
	if m.fmtCursor >= limit {
		start = m.fmtCursor - limit + 1
	}
	for index := start; index < len(m.visible) && index < start+limit; index++ {
		candidate := m.visible[index]
		mark := ""
		if job != nil && candidate.ID == job.VideoID {
			mark = "V"
		}
		if job != nil && candidate.ID == job.AudioID {
			mark = "A"
			if candidate.ID == job.VideoID {
				mark = "V+A"
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
		cells := []string{
			mark,
			candidate.ID,
			string(candidate.Kind),
			candidate.Resolution(),
			firstNonEmpty(candidate.VCodecName(), "—"),
			firstNonEmpty(candidate.ACodecName(), "—"),
			firstNonEmpty(candidate.HDRLabel(), "—"),
			fpsLabel(candidate),
			format.HumanBitrate(candidate.TBR),
			format.HumanSize(candidate.Size),
			candidate.Ext,
			firstNonEmpty(source, "—"),
			note,
		}
		row := joinCells(widths, cells)
		if index == m.fmtCursor {
			row = cursorStyle.Render(truncateANSI(row, m.width))
		}
		lines = append(lines, row)
	}
	return strings.Join(lines, "\n")
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
		return slotStyle.Render(truncate("no row selected", m.width))
	}
	selector := job.Selector()
	if selector == "" {
		selector = "auto (config default)"
	}
	text := fmt.Sprintf("%s  ·  calidad: %s  ·  audio: %s  ·  -f %s  ·  saves to %s",
		job.Label(46), job.QualityLabel(), job.AudioLabel(), selector, m.outdir)
	if video := job.Video(); video != nil && video.Kind == format.Video && job.AudioID == "" {
		text += "  ⚠ no audio selected"
	}
	if m.compatOnly {
		text += fmt.Sprintf("  · only %s-compatible", m.containerLabel())
	}
	return slotStyle.Render(truncate(text, max(10, m.width-2)))
}

func (m *Model) logView() string {
	lines := m.logLines
	if len(lines) > logLinesShown {
		lines = lines[len(lines)-logLinesShown:]
	}
	rendered := make([]string, 0, len(lines)+1)
	rendered = append(rendered, dimStyle.Render("log"))
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
	// The hint bar keeps the quit key visible: segments are dropped from the
	// right until the line fits, and "q quit" is always reserved.
	segments := []string{
		"enter add", "e edit", "v video", "a audio", "d download", "s start",
		"x cancel", "del rm", "c compat", "r reset", "f filter", "l log",
	}
	tail := "q quit"
	width := max(10, m.width)
	if m.notice != "" {
		notice := truncate(m.notice, max(20, width/2))
		keys := truncate(tail, max(5, width-len(notice)-4))
		return warnStyle.Render(notice) + "  " + dimStyle.Render(keys)
	}
	line := tail
	for _, segment := range segments {
		candidate := segment + " · " + line
		if len([]rune(candidate)) > width {
			break
		}
		line = candidate
	}
	return dimStyle.Render(line)
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
