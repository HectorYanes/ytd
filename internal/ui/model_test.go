package ui

import (
	"context"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"ytd/internal/format"
	"ytd/internal/ytdlp"
)

func sizeOf(value int64) *int64 { return &value }

func fixtureFormats() []format.Fmt {
	return []format.Fmt{
		{ID: "137", Ext: "mp4", Kind: format.Video, VCodec: "avc1.640028", Height: 1080, FPS: 30, TBR: 4400, Size: sizeOf(120), Available: true},
		{ID: "399", Ext: "mp4", Kind: format.Video, VCodec: "av01.0.08M.08", Height: 1080, FPS: 60, TBR: 3000, Available: true},
		{ID: "247", Ext: "webm", Kind: format.Video, VCodec: "vp9", Height: 720, FPS: 30, TBR: 1800, Available: true},
		{ID: "140", Ext: "m4a", Kind: format.Audio, ACodec: "mp4a.40.2", ABR: 128, TBR: 128, Size: sizeOf(4), Available: true},
		{ID: "251", Ext: "webm", Kind: format.Audio, ACodec: "opus", ABR: 160, TBR: 160, Available: true},
		{ID: "18", Ext: "mp4", Kind: format.Muxed, VCodec: "avc1.42001E", ACodec: "mp4a.40.2", Height: 360, TBR: 700, Available: true},
		{ID: "sb0", Ext: "mhtml", Kind: format.Image, Note: "storyboard", Available: false},
	}
}

func fixtureResult(title string) ytdlp.AnalyzeResult {
	return ytdlp.AnalyzeResult{
		Info:    format.Info{Type: "video", ID: "abc", Title: title, Uploader: "someone", Duration: 60},
		Formats: fixtureFormats(),
	}
}

func stubAnalyze(result ytdlp.AnalyzeResult, err error) AnalyzeFunc {
	return func(ctx context.Context, url string, flat bool) (ytdlp.AnalyzeResult, error) {
		return result, err
	}
}

func newModel(t *testing.T, options Options) *Model {
	t.Helper()
	if options.MergeFormat == "" {
		options.MergeFormat = "mp4"
	}
	if options.Analyze == nil {
		options.Analyze = stubAnalyze(fixtureResult("A video"), nil)
	}
	model := New(options)
	model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return model
}

func keyMsg(name string) tea.KeyMsg {
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "delete":
		return tea.KeyMsg{Type: tea.KeyDelete}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

func press(model *Model, name string) {
	model.Update(keyMsg(name))
}

// focusQueue moves the keyboard from the URL bar to the queue, which is what
// the user does with Tab or the down arrow.
func focusQueue(model *Model) {
	if model.area != areaQueue {
		press(model, "tab")
	}
}

// analyzedRow adds one URL and delivers its analysis synchronously.
func analyzedRow(t *testing.T, model *Model, url string) {
	t.Helper()
	model.addURL(url)
	uid := model.jobs[len(model.jobs)-1].UID
	model.Update(analyzeMsg{uid: uid, result: fixtureResult("A video")})
	focusQueue(model)
}

func TestInitAnalyzesStartedURLs(t *testing.T) {
	model := newModel(t, Options{StartURLs: []string{"https://example.test/a"}})
	if len(model.jobs) != 1 {
		t.Fatalf("jobs = %d", len(model.jobs))
	}
	if model.jobs[0].Title != "" {
		t.Error("the row should stay unanalyzed until the command runs")
	}
	if model.Init() == nil {
		t.Fatal("Init returned no command")
	}
	// The batch also carries the manager pump, which blocks by design; run the
	// analysis commands the model queued instead of the whole batch.
	if len(model.initCmds) != 1 {
		t.Fatalf("queued analyses = %d, want 1", len(model.initCmds))
	}
	msg, ok := model.initCmds[0]().(analyzeMsg)
	if !ok {
		t.Fatalf("unexpected analysis message %#v", msg)
	}
	model.Update(msg)

	job := model.jobs[0]
	if job.Title != "A video" || len(job.Formats) != 7 {
		t.Errorf("row not filled: %+v", job)
	}
	if job.VideoID == "" || job.AudioID == "" {
		t.Errorf("default selection missing: %q/%q", job.VideoID, job.AudioID)
	}
}

func TestAddURLRejectsDuplicates(t *testing.T) {
	model := newModel(t, Options{})
	model.addURL("https://example.test/a")
	model.addURL("https://example.test/a")
	if len(model.jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(model.jobs))
	}
	if !strings.Contains(model.notice, "already in the queue") {
		t.Errorf("notice = %q", model.notice)
	}
}

func TestAnalysisErrorMarksTheRow(t *testing.T) {
	model := newModel(t, Options{})
	model.addURL("https://example.test/a")
	model.Update(analyzeMsg{uid: 1, err: context.DeadlineExceeded})
	job := model.jobs[0]
	if job.Status != format.Error || !strings.Contains(job.Error, "deadline") {
		t.Errorf("row = %+v", job)
	}
}

func TestFilterNarrowsTheExplorer(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")
	all := len(model.visible)
	model.filterInput.SetValue("1080")
	model.refreshVisible()
	if len(model.visible) >= all || len(model.visible) == 0 {
		t.Fatalf("filter had no effect: %d of %d", len(model.visible), all)
	}
	for _, candidate := range model.visible {
		if candidate.Height != 1080 {
			t.Errorf("row %s does not match the filter", candidate.ID)
		}
	}
	model.filterInput.SetValue("")
	model.refreshVisible()
	if len(model.visible) != all {
		t.Errorf("clearing the filter restored %d of %d rows", len(model.visible), all)
	}
}

func TestCompatToggleHidesIncompatibleStreams(t *testing.T) {
	model := newModel(t, Options{MergeFormat: "mp4"})
	analyzedRow(t, model, "https://example.test/a")

	before := ids(model.visible)
	press(model, "c")
	after := ids(model.visible)
	if len(after) >= len(before) {
		t.Fatalf("compat mode kept %d of %d rows", len(after), len(before))
	}
	for _, id := range after {
		if id == "247" || id == "251" {
			t.Errorf("incompatible format %s survived the filter", id)
		}
	}
	press(model, "c")
	if len(model.visible) != len(before) {
		t.Error("toggling compat back should restore every row")
	}
}

func ids(formats []format.Fmt) []string {
	out := make([]string, len(formats))
	for index, candidate := range formats {
		out[index] = candidate.ID
	}
	return out
}

func TestPickVideoAndAudioFromTheExplorer(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")

	model.fmtCursor = indexOfID(model.visible, "399")
	press(model, "v")
	if model.jobs[0].VideoID != "399" {
		t.Errorf("video = %q", model.jobs[0].VideoID)
	}
	if model.jobs[0].AudioID == "" {
		t.Error("picking a video-only stream must keep the audio choice")
	}

	model.fmtCursor = indexOfID(model.visible, "251")
	press(model, "a")
	if model.jobs[0].AudioID != "251" {
		t.Errorf("audio = %q", model.jobs[0].AudioID)
	}
}

func TestPickingAMuxedStreamClearsTheAudioChoice(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")
	model.jobs[0].AudioID = "140"
	model.fmtCursor = indexOfID(model.visible, "18")
	press(model, "v")
	if model.jobs[0].VideoID != "18" || model.jobs[0].AudioID != "" {
		t.Errorf("choice = %q/%q", model.jobs[0].VideoID, model.jobs[0].AudioID)
	}
}

func TestPickingTheWrongKindIsRejected(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")
	model.jobs[0].VideoID, model.jobs[0].AudioID = "", ""
	model.fmtCursor = indexOfID(model.visible, "251")
	press(model, "v")
	if model.jobs[0].VideoID != "" {
		t.Errorf("an audio row was accepted as video: %q", model.jobs[0].VideoID)
	}
	if !strings.Contains(model.notice, "not a video format") {
		t.Errorf("notice = %q", model.notice)
	}
}

func indexOfID(formats []format.Fmt, id string) int {
	for index, candidate := range formats {
		if candidate.ID == id {
			return index
		}
	}
	return 0
}

func TestResetRestoresTheDefaultSelection(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")
	model.jobs[0].VideoID, model.jobs[0].AudioID = "18", ""
	press(model, "r")
	wantVideo, wantAudio := format.DefaultSelection(model.jobs[0].Formats, "")
	if model.jobs[0].VideoID != wantVideo || model.jobs[0].AudioID != wantAudio {
		t.Errorf("reset = %q/%q, want %q/%q", model.jobs[0].VideoID, model.jobs[0].AudioID, wantVideo, wantAudio)
	}
}

func TestDeleteRemovesTheRow(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")
	analyzedRow(t, model, "https://example.test/b")
	press(model, "delete")
	if len(model.jobs) != 1 || model.jobs[0].URL != "https://example.test/a" {
		t.Errorf("jobs after delete = %+v", model.jobs)
	}
	press(model, "delete")
	if len(model.jobs) != 0 {
		t.Errorf("jobs after deleting everything = %d", len(model.jobs))
	}
	if model.focusedJob() != nil {
		t.Error("an empty queue must have no focused row")
	}
}

func TestEditorAppliesTheChosenStreams(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")

	press(model, "e")
	if model.editor == nil {
		t.Fatal("editor did not open")
	}
	editor := model.editor
	editor.videoSel = indexOfID(editor.video, "399")
	editor.audioSel = indexOfID(editor.audio, "140")
	press(model, "enter")

	if model.editor != nil {
		t.Error("editor stayed open after Enter")
	}
	if model.jobs[0].VideoID != "399" || model.jobs[0].AudioID != "140" {
		t.Errorf("editor result = %q/%q", model.jobs[0].VideoID, model.jobs[0].AudioID)
	}
}

func TestEditorEscLeavesTheChoiceAlone(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")
	before := model.jobs[0].VideoID + "/" + model.jobs[0].AudioID
	press(model, "e")
	model.editor.videoSel = indexOfID(model.editor.video, "137")
	press(model, "esc")
	if model.editor != nil {
		t.Error("Esc should close the editor")
	}
	if got := model.jobs[0].VideoID + "/" + model.jobs[0].AudioID; got != before {
		t.Errorf("choice changed on cancel: %s", got)
	}
}

func TestEditorHidesIncompatibleFormatsUntilAsked(t *testing.T) {
	model := newModel(t, Options{MergeFormat: "mp4"})
	analyzedRow(t, model, "https://example.test/a")
	press(model, "e")
	for _, candidate := range model.editor.video {
		if candidate.ID == "247" {
			t.Fatal("vp9 offered for an mp4 merge")
		}
	}
	press(model, "i")
	found := false
	for _, candidate := range model.editor.video {
		if candidate.ID == "247" {
			found = true
		}
	}
	if !found {
		t.Error("toggling incompatible formats did not reveal vp9")
	}
}

func TestUnanalyzedRowAsksForAnalysisFirst(t *testing.T) {
	model := newModel(t, Options{})
	model.addURL("https://example.test/a")
	model.startEdit()
	if model.editor != nil {
		t.Error("the editor opened on an unanalyzed row")
	}
	if !strings.Contains(model.notice, "Analyzing") {
		t.Errorf("notice = %q", model.notice)
	}
}

func TestPlaylistExpandsIntoOneRowPerEntry(t *testing.T) {
	model := newModel(t, Options{})
	model.addURL("https://example.test/list")
	model.Update(analyzeMsg{uid: 1, result: ytdlp.AnalyzeResult{
		Info: format.Info{Type: "playlist", Title: "A list", Entries: []format.Info{
			{ID: "aaa", Title: "first", Duration: 30},
			{ID: "bbb", Title: "second", Availability: "private"},
		}},
	}})
	if len(model.jobs) != 2 {
		t.Fatalf("jobs = %d, want 2", len(model.jobs))
	}
	if model.jobs[0].Title != "first" || model.jobs[0].URL != "https://www.youtube.com/watch?v=aaa" {
		t.Errorf("first row = %+v", model.jobs[0])
	}
	if model.jobs[1].Status != format.Error || !strings.Contains(model.jobs[1].Error, "private") {
		t.Errorf("private row = %+v", model.jobs[1])
	}
	if !strings.Contains(model.notice, "2 rows") {
		t.Errorf("notice = %q", model.notice)
	}
}

func TestPlaylistKeepsExistingRows(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/keep")
	model.addURL("https://example.test/list")
	model.Update(analyzeMsg{uid: 2, result: ytdlp.AnalyzeResult{
		Info: format.Info{Type: "playlist", Title: "A list", Entries: []format.Info{
			{ID: "keep", Title: "already here", URL: "https://example.test/keep"},
			{ID: "new", Title: "new one"},
		}},
	}})
	if len(model.jobs) != 2 {
		t.Fatalf("jobs = %d, want the existing row plus one new", len(model.jobs))
	}
	if model.jobs[0].URL != "https://example.test/keep" || model.jobs[1].URL != "https://www.youtube.com/watch?v=new" {
		t.Errorf("rows = %+v", model.jobs)
	}
}

func TestManagerSnapshotUpdatesTheRowAndLog(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")

	snapshot := model.jobs[0]
	snapshot.Status = format.Downloading
	snapshot.Percent = 42
	model.Update(managerMsg{job: snapshot})
	if model.jobs[0].Status != format.Downloading || model.jobs[0].Percent != 42 {
		t.Errorf("row = %+v", model.jobs[0])
	}
	if len(model.jobs[0].Formats) == 0 {
		t.Error("a snapshot must not wipe the formats the UI owns")
	}

	snapshot.Status = format.Done
	snapshot.Percent = 100
	snapshot.Path = "/out/a.mp4"
	model.Update(managerMsg{job: snapshot})
	if model.jobs[0].Status != format.Done {
		t.Errorf("status = %q", model.jobs[0].Status)
	}
	found := false
	for _, entry := range model.logLines {
		if entry.level == "ok" && strings.Contains(entry.text, "done") {
			found = true
		}
	}
	if !found {
		t.Errorf("no done entry in the log: %+v", model.logLines)
	}
}

func TestStartAllQueuesPendingRows(t *testing.T) {
	model := newModel(t, Options{Spawn: func(ctx context.Context, cmd, env []string) (ytdlp.Proc, error) {
		return &yieldingProc{}, nil
	}})
	analyzedRow(t, model, "https://example.test/a")
	analyzedRow(t, model, "https://example.test/b")
	model.jobs[1].Status = format.Error

	model.startAll()
	if !model.manager.Busy() {
		t.Error("the manager did not receive the pending rows")
	}
	model.manager.Wait()
}

type yieldingProc struct{}

func (y *yieldingProc) Read(buffer []byte) (int, error) { return 0, io.EOF }
func (y *yieldingProc) Wait() error                     { return nil }
func (y *yieldingProc) Kill() error                     { return nil }

func TestCancelOnlyAppliesToDownloadingRows(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")
	press(model, "x")
	if !strings.Contains(model.notice, "Nothing to cancel") {
		t.Errorf("notice = %q", model.notice)
	}
}

func TestViewShowsTheQueueAndTheFooter(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")
	view := model.View()
	for _, want := range []string{"ytd — yt-dlp queue", "título", "calidad", "cargada", "1080p", "quit"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}
}

func TestViewIsEmptyQueueSafe(t *testing.T) {
	model := newModel(t, Options{})
	view := model.View()
	if !strings.Contains(view, "(empty") || !strings.Contains(view, "no row selected") {
		t.Errorf("empty queue view:\n%s", view)
	}
}

func TestLogPaneToggles(t *testing.T) {
	model := newModel(t, Options{})
	focusQueue(model)
	if model.showLog {
		t.Error("the log should start hidden")
	}
	press(model, "l")
	if !model.showLog {
		t.Error("l did not show the log")
	}
	if !strings.Contains(model.View(), "yt-dlp:") {
		t.Errorf("log content missing from the view:\n%s", model.View())
	}
}

func TestFilterAndUrlAreasTakePrintableKeys(t *testing.T) {
	model := newModel(t, Options{})
	focusQueue(model)
	press(model, "f")
	if model.area != areaFilter {
		t.Fatal("f did not focus the filter")
	}
	press(model, "1")
	press(model, "0")
	if model.filterInput.Value() != "10" {
		t.Errorf("filter = %q", model.filterInput.Value())
	}
	press(model, "esc")
	if model.area != areaQueue {
		t.Error("esc should return focus to the queue")
	}
	press(model, "q")
	if !model.quitting {
		t.Error("q should quit from the queue")
	}
}

func TestURLBarAcceptsSeveralURLs(t *testing.T) {
	model := newModel(t, Options{})
	model.urlInput.SetValue("https://example.test/a https://example.test/b")
	press(model, "enter")
	if len(model.jobs) != 2 {
		t.Fatalf("jobs = %d, want 2", len(model.jobs))
	}
	if model.urlInput.Value() != "" {
		t.Errorf("the bar was not cleared: %q", model.urlInput.Value())
	}
}

func TestMissingJSRuntimeIsReported(t *testing.T) {
	model := newModel(t, Options{})
	// The machine running these tests may or may not have a runtime; assert that
	// the header/notice path renders whatever was detected.
	if model.jsRuntime == "" {
		if !strings.Contains(model.View(), "JavaScript runtime") {
			t.Error("the view should warn about the missing runtime")
		}
	}
}
