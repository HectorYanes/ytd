// Package ui is the bubbletea interface: a URL bar, a queue, a format explorer
// and a log pane, mirroring the layout the Textual version had.
package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"ytd/internal/format"
	"ytd/internal/ytdlp"
)

// AnalyzeFunc fetches the JSON for one URL; swapped out in tests.
type AnalyzeFunc func(ctx context.Context, url string, flat bool) (ytdlp.AnalyzeResult, error)

// Options configures the application.
type Options struct {
	OutDir        string
	StartURLs     []string
	MaxConcurrent int
	MergeFormat   string // "" means: read the user's yt-dlp config
	Spawn         ytdlp.SpawnFunc
	Analyze       AnalyzeFunc
	Seed          []format.Job // pre-analyzed rows, used by tests
}

var (
	kindOptions      = []string{"all", string(format.Muxed), string(format.Video), string(format.Audio), string(format.Image)}
	containerOptions = []string{"all", "mp4", "webm", "m4a"}

	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))
	dimStyle      = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("8"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	warnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	accentStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	audioStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	hdrStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	slotStyle     = lipgloss.NewStyle().Background(lipgloss.Color("234")).Padding(0, 1)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))
	progressStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
)

// area is which widget owns printable keys.
type area int

const (
	areaURL area = iota
	areaQueue
	areaFormats
	areaFilter
)

type logEntry struct {
	text  string
	level string
}

// panelHit is one scrollable table's on-screen window: the first terminal line
// that draws data, how many lines it draws, and which logical row sits on top.
type panelHit struct {
	top   int
	rows  int
	start int
}

func (p panelHit) rowAt(y int) (int, bool) {
	if !p.inside(y) {
		return 0, false
	}
	return p.start + (y - p.top), true
}

func (p panelHit) inside(y int) bool {
	return p.rows > 0 && y >= p.top && y < p.top+p.rows
}

// insideLoose also accepts the panel header line, so a wheel over it scrolls.
func (p panelHit) insideLoose(y int) bool {
	return p.rows > 0 && y >= p.top-1 && y < p.top+p.rows
}

// hitRegions is where the widgets ended up on the last render.
type hitRegions struct {
	queue   panelHit
	formats panelHit
	url     int
	filter  int
}

// Model is the whole application state.
type Model struct {
	outdir        string
	mergeFormat   string
	maxConcurrent int
	ffmpeg        string
	ytdlpOrigin   string
	jsRuntime     string

	width  int
	height int

	jobs  []format.Job
	focus int

	urlInput    textinput.Model
	filterInput textinput.Model
	area        area
	kindIdx     int
	contIdx     int
	onlySize    bool
	hideHLS     bool
	compatOnly  bool
	showLog     bool

	visible   []format.Fmt
	fmtCursor int

	editor *editor

	// hit remembers the on-screen regions of the queue and format tables so mouse
	// events land on the right row. It is recomputed on every render.
	hit hitRegions

	logLines []logEntry
	notice   string
	noticeAt time.Time

	manager   *ytdlp.Manager
	updates   chan format.Job
	pending   map[int]bool
	analyzeFn AnalyzeFunc
	initCmds  []tea.Cmd
	quitting  bool
}

// New builds the model; Init returns the commands it queues.
func New(options Options) *Model {
	if options.OutDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		options.OutDir = home + "/Downloads"
	}
	if options.MaxConcurrent < 1 {
		options.MaxConcurrent = 1
	}
	mergeFormat := options.MergeFormat
	if mergeFormat == "" {
		mergeFormat = ytdlp.DetectUserMergeFormat()
	}
	analyze := options.Analyze
	if analyze == nil {
		analyze = func(ctx context.Context, url string, flat bool) (ytdlp.AnalyzeResult, error) {
			return ytdlp.Analyze(ctx, url, flat, 180*time.Second)
		}
	}

	urlInput := textinput.New()
	urlInput.Placeholder = "Paste a URL (or several, space separated) and press Enter"
	urlInput.Prompt = "› "
	urlInput.CharLimit = 0
	filterInput := textinput.New()
	filterInput.Placeholder = "filter: 1080, av1, opus, muxed…"
	filterInput.Prompt = "/ "

	m := &Model{
		outdir:        options.OutDir,
		mergeFormat:   mergeFormat,
		maxConcurrent: options.MaxConcurrent,
		ffmpeg:        ytdlp.FFmpegPath(),
		ytdlpOrigin:   "",
		jsRuntime:     ytdlp.JSRuntime(nil),
		width:         100,
		height:        32,
		urlInput:      urlInput,
		filterInput:   filterInput,
		hideHLS:       false,
		updates:       make(chan format.Job, 256),
		pending:       map[int]bool{},
		analyzeFn:     analyze,
	}

	if resolution, err := ytdlp.Resolve(); err == nil {
		m.ytdlpOrigin = resolution.Origin
	} else {
		m.ytdlpOrigin = "missing — run ytd --update"
	}

	m.manager = ytdlp.NewManager(m.outdir, m.maxConcurrent, func(job format.Job) {
		// Progress snapshots are cheap: dropping one under load is fine, the
		// next update carries the same or newer state.
		select {
		case m.updates <- job:
		default:
		}
	})
	if resolution, err := ytdlp.Resolve(); err == nil {
		m.manager.Prefix = resolution.Prefix
	}
	if options.Spawn != nil {
		m.manager.Spawn = options.Spawn
	}

	for _, job := range options.Seed {
		job := job
		m.jobs = append(m.jobs, job)
	}
	m.nextUID()
	for _, url := range options.StartURLs {
		m.initCmds = append(m.initCmds, m.addURL(url)...)
	}

	m.writeLog("dim", fmt.Sprintf("yt-dlp: %s", m.ytdlpOrigin))
	if m.ffmpeg != "" {
		m.writeLog("dim", fmt.Sprintf("ffmpeg: %s", m.ffmpeg))
	} else {
		m.writeLog("warn", "ffmpeg is not installed: merging separate video+audio streams and embedding metadata/thumbnails will fail.")
	}
	if m.jsRuntime == "" {
		m.writeLog("warn", "No JavaScript runtime found: YouTube will not list formats. Install deno or node, or run ytd --update.")
	}
	m.refreshDerived()
	return m
}

func (m *Model) nextUID() int {
	max := 0
	for _, job := range m.jobs {
		if job.UID > max {
			max = job.UID
		}
	}
	return max + 1
}

// Init focuses the URL bar and starts the manager pump.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.urlInput.Focus(), m.waitUpdate()}
	cmds = append(cmds, m.initCmds...)
	return tea.Batch(cmds...)
}

// ---------------------------------------------------------------------------
// messages
// ---------------------------------------------------------------------------

type managerMsg struct{ job format.Job }

type analyzeMsg struct {
	uid    int
	result ytdlp.AnalyzeResult
	err    error
}

type noticeExpired struct{ at time.Time }

func (m *Model) waitUpdate() tea.Cmd {
	return func() tea.Msg {
		job, ok := <-m.updates
		if !ok {
			return nil
		}
		return managerMsg{job: job}
	}
}

func (m *Model) analyzeCmd(uid int, url string) tea.Cmd {
	analyze := m.analyzeFn
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		result, err := analyze(ctx, url, true)
		return analyzeMsg{uid: uid, result: result, err: err}
	}
}

// ---------------------------------------------------------------------------
// update
// ---------------------------------------------------------------------------

// Update handles one message.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = typed.Width, typed.Height
		m.urlInput.Width = max(20, typed.Width-6)
		m.filterInput.Width = max(20, typed.Width-30)
		return m, nil

	case managerMsg:
		m.applySnapshot(typed.job)
		return m, m.waitUpdate()

	case analyzeMsg:
		delete(m.pending, typed.uid)
		return m, m.applyAnalysis(typed)

	case noticeExpired:
		if typed.at.Equal(m.noticeAt) {
			m.notice = ""
		}
		return m, nil

	case tea.KeyMsg:
		if typed.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
		return m, m.handleKey(typed)
	case tea.MouseMsg:
		return m, m.handleMouse(typed)
	}
	return m, nil
}

func (m *Model) applySnapshot(job format.Job) {
	for index := range m.jobs {
		if m.jobs[index].UID != job.UID {
			continue
		}
		previous := m.jobs[index].Status
		job.Formats = m.jobs[index].Formats // formats live in the UI, not the manager
		m.jobs[index] = job
		if previous != job.Status {
			m.onStatusChange(job)
		}
		break
	}
	m.refreshDerived()
}

func (m *Model) onStatusChange(job format.Job) {
	switch job.Status {
	case format.Downloading:
		m.showLog = true
		m.writeLog("dim", fmt.Sprintf("downloading %s", job.Label(60)))
	case format.Done:
		m.writeLog("ok", fmt.Sprintf("done %s → %s", job.Label(60), job.Path))
	case format.Error:
		m.writeLog("error", fmt.Sprintf("failed %s: %s", job.Label(40), job.Error))
	case format.Cancelled:
		m.writeLog("warn", fmt.Sprintf("cancelled %s", job.Label(60)))
	}
}

func (m *Model) applyAnalysis(msg analyzeMsg) tea.Cmd {
	job := m.jobByUID(msg.uid)
	if job == nil {
		return nil
	}
	if msg.err != nil {
		job.Status = format.Error
		job.Error = truncate(msg.err.Error(), 110)
		m.writeLog("error", fmt.Sprintf("could not analyze %s: %s", job.URL, job.Error))
		m.notify("Could not analyze: " + job.Error)
		m.refreshDerived()
		return nil
	}
	if warning := lastLine(msg.result.Warnings); warning != "" {
		m.writeLog("warn", "yt-dlp warned: "+truncate(warning, 150))
	}
	expanded := m.absorb(job, msg.result)
	m.refreshDerived()
	if expanded != nil {
		return tea.Batch(expanded...)
	}
	if job.Status == format.Error {
		m.notify(job.Error)
	}
	return nil
}

// absorb fills a row from an analysis result; a playlist is expanded into one
// row per entry and the analyses for those rows are returned.
func (m *Model) absorb(job *format.Job, result ytdlp.AnalyzeResult) []tea.Cmd {
	if result.Info.Type == "playlist" {
		return m.expandPlaylist(job, result.Info)
	}
	if result.Info.Title != "" {
		job.Title = result.Info.Title
	} else if result.Info.ID != "" {
		job.Title = result.Info.ID
	} else {
		job.Title = job.URL
	}
	job.Uploader = result.Info.Uploader
	job.Duration = result.Info.Duration
	job.Formats = result.Formats
	if job.VideoID == "" && job.AudioID == "" {
		job.VideoID, job.AudioID = format.DefaultSelection(job.Formats, "")
	}
	return nil
}

func (m *Model) expandPlaylist(job *format.Job, info format.Info) []tea.Cmd {
	entries := make([]format.Info, 0, len(info.Entries))
	for _, entry := range info.Entries {
		if entry.ID != "" || entry.URL != "" || entry.WebpageURL != "" {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		job.Status = format.Error
		job.Error = "empty playlist"
		return nil
	}
	index := m.indexOf(job.UID)
	m.jobs = append(m.jobs[:index], m.jobs[index+1:]...)

	var added []format.Job
	var cmds []tea.Cmd
	for _, entry := range entries {
		url := firstNonEmpty(entry.URL, entry.WebpageURL)
		if url == "" && entry.ID != "" {
			url = "https://www.youtube.com/watch?v=" + entry.ID
		}
		if url == "" || m.hasURL(url) {
			continue
		}
		fresh := format.NewJob(m.nextUID(), url)
		fresh.Title = firstNonEmpty(entry.Title, url)
		fresh.Uploader = firstNonEmpty(entry.Uploader, info.Uploader)
		fresh.Duration = entry.Duration
		switch entry.Availability {
		case "private", "premium_only", "subscriber_only":
			fresh.Status = format.Error
			fresh.Error = fmt.Sprintf("unavailable (%s)", entry.Availability)
		}
		added = append(added, *fresh)
	}
	m.jobs = append(m.jobs[:index], append(added, m.jobs[index:]...)...)
	if index < len(m.jobs) && len(added) > 0 {
		m.focus = index
	} else {
		m.focus = clampFocus(m.focus, m.jobs)
	}
	title := firstNonEmpty(info.Title, "playlist")
	m.writeLog("dim", fmt.Sprintf("playlist '%s': %d row(s)", title, len(added)))
	if len(added) > 0 {
		m.notify(fmt.Sprintf("%d rows from %s", len(added), title))
	} else {
		m.notify("Nothing to add.")
	}
	for _, element := range added {
		if element.Status == format.Error {
			continue
		}
		uid := element.UID
		m.pending[uid] = true
		cmds = append(cmds, m.analyzeCmd(uid, element.URL))
	}
	return cmds
}

// ---------------------------------------------------------------------------
// keys
// ---------------------------------------------------------------------------

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	if m.editor != nil {
		return m.handleEditorKey(msg)
	}
	switch m.area {
	case areaURL:
		switch msg.String() {
		case "q":
			// With nothing typed, q is a quit rather than a stray character;
			// once there is text the bar keeps it so a URL is never lost.
			if m.urlInput.Value() == "" {
				m.quitting = true
				return tea.Quit
			}
		case "enter":
			raw := strings.TrimSpace(m.urlInput.Value())
			if raw == "" {
				return nil
			}
			m.urlInput.SetValue("")
			var cmds []tea.Cmd
			for _, url := range strings.Fields(raw) {
				cmds = append(cmds, m.addURL(url)...)
			}
			if len(cmds) > 0 {
				m.setArea(areaFormats)
			}
			return tea.Batch(cmds...)
		case "down", "tab":
			m.setArea(areaQueue)
			return nil
		case "esc":
			m.urlInput.Blur()
			return nil
		}
		var cmd tea.Cmd
		m.urlInput, cmd = m.urlInput.Update(msg)
		return cmd
	case areaFilter:
		switch msg.String() {
		case "esc", "enter":
			m.setArea(areaQueue)
			return nil
		case "tab":
			return m.cycleArea(true)
		case "shift+tab":
			return m.cycleArea(false)
		}
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		m.refreshVisible()
		return cmd
	}
	return m.handleBodyKey(msg)
}

func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if msg.Action == tea.MouseActionPress {
			return m.scrollMouse(msg.Y, -3)
		}
	case tea.MouseButtonWheelDown:
		if msg.Action == tea.MouseActionPress {
			return m.scrollMouse(msg.Y, 3)
		}
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	switch {
	case m.hit.url > 0 && msg.Y == m.hit.url:
		m.setArea(areaURL)
	case m.hit.filter >= 0 && msg.Y == m.hit.filter:
		m.setArea(areaFilter)
	case m.hit.queue.inside(msg.Y):
		if index, ok := m.hit.queue.rowAt(msg.Y); ok && index >= 0 && index < len(m.jobs) {
			if m.focus != index {
				m.focus = index
				m.fmtCursor = 0
				m.refreshDerived()
			}
			m.setArea(areaQueue)
		}
	case m.hit.formats.inside(msg.Y):
		if index, ok := m.hit.formats.rowAt(msg.Y); ok && index >= 0 && index < len(m.visible) {
			m.fmtCursor = index
			m.setArea(areaFormats)
		}
	}
	return nil
}

// scrollMouse routes a wheel event to the panel under the pointer.
func (m *Model) scrollMouse(y, delta int) tea.Cmd {
	switch {
	case m.hit.formats.insideLoose(y):
		m.setArea(areaFormats)
		m.moveFormatCursor(delta)
	case m.hit.queue.insideLoose(y):
		m.setArea(areaQueue)
		if delta > 0 {
			m.moveFocus(1)
		} else {
			m.moveFocus(-1)
		}
	}
	return nil
}

// setArea moves keyboard focus to one pane and manages text input focus.
func (m *Model) setArea(target area) {
	m.area = target
	switch target {
	case areaURL:
		m.filterInput.Blur()
		m.urlInput.Focus()
	case areaFilter:
		m.urlInput.Blur()
		m.filterInput.Focus()
	default:
		m.urlInput.Blur()
		m.filterInput.Blur()
	}
}

// cycleArea moves focus through URL → queue → formats → filter and back.
func (m *Model) cycleArea(forward bool) tea.Cmd {
	order := []area{areaURL, areaQueue, areaFormats, areaFilter}
	index := 0
	for i, candidate := range order {
		if candidate == m.area {
			index = i
			break
		}
	}
	if forward {
		index = (index + 1) % len(order)
	} else {
		index = (index - 1 + len(order)) % len(order)
	}
	target := order[index]
	m.setArea(target)
	if target == areaFilter {
		return m.filterInput.Focus()
	}
	return nil
}

func (m *Model) handleBodyKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	switch key {
	case "q":
		m.quitting = true
		return tea.Quit
	case "tab":
		return m.cycleArea(true)
	case "shift+tab":
		return m.cycleArea(false)
	case "f", "/":
		m.setArea(areaFilter)
		return m.filterInput.Focus()
	}

	if m.area == areaFormats {
		switch key {
		case "up", "k":
			m.moveFormatCursor(-1)
		case "down", "j":
			m.moveFormatCursor(1)
		case "pgup":
			m.moveFormatCursor(-8)
		case "pgdown":
			m.moveFormatCursor(8)
		case "home", "g":
			m.fmtCursor = 0
		case "end", "G":
			if len(m.visible) > 0 {
				m.fmtCursor = len(m.visible) - 1
			}
		case "left", "esc":
			m.setArea(areaQueue)
		}
	} else {
		switch key {
		case "up", "k":
			m.moveFocus(-1)
		case "down", "j":
			m.moveFocus(1)
		case "pgup":
			m.moveFocus(-6)
		case "pgdown":
			m.moveFocus(6)
		case "home", "g":
			m.focus = 0
			m.refreshDerived()
		case "end", "G":
			if len(m.jobs) > 0 {
				m.focus = len(m.jobs) - 1
			}
			m.refreshDerived()
		case "right", "enter":
			m.setArea(areaFormats)
		}
	}

	switch key {
	case "v":
		if !m.pickVideo(m.currentFormatID()) {
			m.notify("That row is not a video format.")
		}
	case "a":
		if !m.pickAudio(m.currentFormatID()) {
			m.notify("That row is not an audio-only format.")
		}
	case "e":
		return m.startEdit()
	case "d":
		return m.download()
	case "s":
		return m.startAll()
	case "x":
		return m.cancelJob()
	case "delete", "backspace":
		m.deleteJob()
	case "c":
		m.compatOnly = !m.compatOnly
		m.refreshVisible()
		if m.compatOnly {
			m.notify(fmt.Sprintf("Showing only %s-compatible formats.", m.containerLabel()))
		} else {
			m.notify("Showing all formats.")
		}
	case "r":
		if job := m.focusedJob(); job != nil {
			job.VideoID, job.AudioID = format.DefaultSelection(job.Formats, "")
			m.refreshDerived()
		}
	case "l":
		m.showLog = !m.showLog
	case "i":
		m.cycleKind()
	case "o":
		m.cycleContainer()
	case "h":
		m.hideHLS = !m.hideHLS
		m.refreshVisible()
	case "p":
		m.onlySize = !m.onlySize
		m.refreshVisible()
	}
	return nil
}

func (m *Model) moveFormatCursor(delta int) {
	if len(m.visible) == 0 {
		m.fmtCursor = 0
		return
	}
	m.fmtCursor = clamp(m.fmtCursor+delta, 0, len(m.visible)-1)
}

func (m *Model) moveFocus(delta int) {
	if len(m.jobs) == 0 {
		return
	}
	m.focus = clamp(m.focus+delta, 0, len(m.jobs)-1)
	m.fmtCursor = 0
	m.refreshDerived()
}

func (m *Model) cycleKind() {
	m.kindIdx = (m.kindIdx + 1) % len(kindOptions)
	m.refreshVisible()
}

func (m *Model) cycleContainer() {
	m.contIdx = (m.contIdx + 1) % len(containerOptions)
	m.refreshVisible()
}

// ---------------------------------------------------------------------------
// job operations
// ---------------------------------------------------------------------------

func (m *Model) addURL(url string) []tea.Cmd {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil
	}
	if m.hasURL(url) {
		m.notify("That URL is already in the queue.")
		return nil
	}
	job := format.NewJob(m.nextUID(), url)
	m.jobs = append(m.jobs, *job)
	m.focus = len(m.jobs) - 1
	m.fmtCursor = 0
	m.pending[job.UID] = true
	m.writeLog("dim", "$ yt-dlp -J --flat-playlist "+url)
	m.refreshDerived()
	return []tea.Cmd{m.analyzeCmd(job.UID, url)}
}

func (m *Model) hasURL(url string) bool {
	for _, job := range m.jobs {
		if job.URL == url {
			return true
		}
	}
	return false
}

func (m *Model) indexOf(uid int) int {
	for index, job := range m.jobs {
		if job.UID == uid {
			return index
		}
	}
	return -1
}

func (m *Model) jobByUID(uid int) *format.Job {
	if index := m.indexOf(uid); index >= 0 {
		return &m.jobs[index]
	}
	return nil
}

func (m *Model) focusedJob() *format.Job {
	if len(m.jobs) == 0 || m.focus < 0 || m.focus >= len(m.jobs) {
		return nil
	}
	return &m.jobs[m.focus]
}

func (m *Model) startEdit() tea.Cmd {
	job := m.focusedJob()
	if job == nil {
		return nil
	}
	if !job.Analyzed() {
		m.notify("Analyzing… press e again to edit this row.")
		if !m.pending[job.UID] {
			m.pending[job.UID] = true
			return m.analyzeCmd(job.UID, job.URL)
		}
		return nil
	}
	m.editor = newEditor(*job, m.mergeFormat)
	return nil
}

func (m *Model) download() tea.Cmd {
	job := m.focusedJob()
	if job == nil {
		m.notify("Nothing selected.")
		return nil
	}
	if job.Status == format.Downloading {
		m.notify("Already downloading.")
		return nil
	}
	if m.manager.Submit(*job) > 0 {
		m.showLog = true
		m.writeLog("dim", "queued "+job.Label(50))
	}
	return nil
}

func (m *Model) startAll() tea.Cmd {
	var pending []format.Job
	for _, job := range m.jobs {
		switch job.Status {
		case format.Loaded, format.Error, format.Cancelled:
			pending = append(pending, job)
		}
	}
	if len(pending) == 0 {
		m.notify("Nothing pending.")
		return nil
	}
	added := m.manager.Submit(pending...)
	m.showLog = true
	m.writeLog("dim", fmt.Sprintf("queued %d job(s), concurrency %d", added, m.maxConcurrent))
	return nil
}

func (m *Model) cancelJob() tea.Cmd {
	job := m.focusedJob()
	if job == nil || job.Status != format.Downloading {
		m.notify("Nothing to cancel.")
		return nil
	}
	job.Status = format.Cancelled
	m.manager.Cancel(job.UID)
	m.refreshDerived()
	return nil
}

func (m *Model) deleteJob() {
	job := m.focusedJob()
	if job == nil {
		return
	}
	if job.Status == format.Downloading {
		job.Status = format.Cancelled
		m.manager.Cancel(job.UID)
	}
	index := m.focus
	m.jobs = append(m.jobs[:index], m.jobs[index+1:]...)
	m.focus = clampFocus(index, m.jobs)
	m.fmtCursor = 0
	m.refreshDerived()
}

func (m *Model) currentFormatID() string {
	if m.fmtCursor < 0 || m.fmtCursor >= len(m.visible) {
		return ""
	}
	return m.visible[m.fmtCursor].ID
}

func (m *Model) pickVideo(formatID string) bool {
	job := m.focusedJob()
	if job == nil || formatID == "" {
		return false
	}
	candidate := job.Find(formatID)
	if candidate == nil || (candidate.Kind != format.Video && candidate.Kind != format.Muxed) {
		return false
	}
	job.VideoID = candidate.ID
	if candidate.Kind == format.Muxed {
		job.AudioID = ""
	}
	m.refreshDerived()
	return true
}

func (m *Model) pickAudio(formatID string) bool {
	job := m.focusedJob()
	if job == nil || formatID == "" {
		return false
	}
	candidate := job.Find(formatID)
	if candidate == nil || candidate.Kind != format.Audio {
		return false
	}
	job.AudioID = candidate.ID
	m.refreshDerived()
	return true
}

// ---------------------------------------------------------------------------
// derived state
// ---------------------------------------------------------------------------

func (m *Model) refreshDerived() {
	m.focus = clampFocus(m.focus, m.jobs)
	m.refreshVisible()
}

func (m *Model) currentFilters() format.Filters {
	filters := format.Filters{
		Text:         m.filterInput.Value(),
		OnlyWithSize: m.onlySize,
		HideHLS:      m.hideHLS,
	}
	if m.kindIdx > 0 && m.kindIdx < len(kindOptions) {
		filters.Kind = kindOptions[m.kindIdx]
	}
	if m.contIdx > 0 && m.contIdx < len(containerOptions) {
		filters.Container = containerOptions[m.contIdx]
	}
	return filters
}

func (m *Model) refreshVisible() {
	job := m.focusedJob()
	if job == nil {
		m.visible = nil
		m.fmtCursor = 0
		return
	}
	visible := format.ApplyFilters(job.Formats, m.currentFilters())
	if m.compatOnly {
		kept := visible[:0]
		for _, candidate := range visible {
			if format.IsCompatible(candidate, m.mergeFormat) {
				kept = append(kept, candidate)
			}
		}
		visible = kept
	}
	m.visible = visible
	if m.fmtCursor >= len(visible) {
		m.fmtCursor = max(0, len(visible)-1)
	}
}

func (m *Model) containerLabel() string {
	if m.mergeFormat == "" {
		return "compatible"
	}
	return m.mergeFormat
}

func (m *Model) writeLog(level, text string) {
	m.logLines = append(m.logLines, logEntry{text: text, level: level})
	if len(m.logLines) > 200 {
		m.logLines = m.logLines[len(m.logLines)-200:]
	}
}

func (m *Model) notify(text string) {
	m.notice = text
	m.noticeAt = time.Now()
}

// NoticeCmd clears the footer notice after a delay.
func (m *Model) NoticeCmd() tea.Cmd {
	at := m.noticeAt
	return tea.Tick(6*time.Second, func(time.Time) tea.Msg { return noticeExpired{at: at} })
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func clampFocus(focus int, jobs []format.Job) int {
	if len(jobs) == 0 {
		return 0
	}
	return clamp(focus, 0, len(jobs)-1)
}

func clamp(value, low, high int) int {
	return max(low, min(high, value))
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:max(1, limit-1)]) + "…"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
