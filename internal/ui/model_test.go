package ui

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"ytd/internal/format"
	"ytd/internal/store"
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
	if options.StatePath == "" {
		options.StatePath = filepath.Join(t.TempDir(), "queue.json")
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
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
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
	analyzedRowWith(t, model, url, fixtureResult("A video"))
}

// analyzedRowWith is analyzedRow with a custom analysis result.
func analyzedRowWith(t *testing.T, model *Model, url string, result ytdlp.AnalyzeResult) {
	t.Helper()
	model.addURL(url)
	uid := model.jobs[len(model.jobs)-1].UID
	model.Update(analyzeMsg{uid: uid, result: result})
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
	// mkv merges anything, so every stream reaches the filter.
	model := newModel(t, Options{MergeFormat: "mkv"})
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
	// The container filter normally drops these rows already; the fallback path
	// (nothing matches the container) is where the toggle still has work to do.
	webmOnly := []format.Fmt{
		{ID: "247", Ext: "webm", Kind: format.Video, VCodec: "vp9", Height: 720, Available: true},
	}
	model := newModel(t, Options{MergeFormat: "mp4"})
	analyzedRowWith(t, model, "https://example.test/a", ytdlp.AnalyzeResult{
		Info:    format.Info{Type: "video", ID: "abc", Title: "A video"},
		Formats: webmOnly,
	})
	if len(model.visible) != 1 {
		t.Fatalf("the fallback should still offer the webm stream, got %v", ids(model.visible))
	}
	press(model, "c")
	if len(model.visible) != 0 {
		t.Errorf("compat mode kept an unmergeable stream: %v", ids(model.visible))
	}
	press(model, "c")
	if len(model.visible) != 1 {
		t.Error("toggling compat back should restore the row")
	}
}

func ids(formats []format.Fmt) []string {
	out := make([]string, len(formats))
	for index, candidate := range formats {
		out[index] = candidate.ID
	}
	return out
}

func sameIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func TestExplorerShowsVideosAboveAndMuxedBelow(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")
	press(model, "right")

	if got := ids(model.visible); !sameIDs(got, []string{"137", "399"}) {
		t.Fatalf("top rows = %v, want the mp4 video streams", got)
	}
	if got := ids(model.muxed); !sameIDs(got, []string{"18"}) {
		t.Errorf("muxed rows = %v", got)
	}
	model.View()
	if model.hit.formats.rows == 0 || model.hit.muxed.rows == 0 {
		t.Fatalf("both tables must be laid out: videos=%d muxed=%d",
			model.hit.formats.rows, model.hit.muxed.rows)
	}
	if model.hit.muxed.rows >= model.hit.formats.rows {
		t.Errorf("the muxed table takes %d rows, the video table only %d",
			model.hit.muxed.rows, model.hit.formats.rows)
	}

	view := model.View()
	if lineContaining(view, "╭─ videos") == "" {
		t.Errorf("no video table:\n%s", view)
	}
	if lineContaining(view, "╭─ muxed") == "" {
		t.Errorf("no muxed table:\n%s", view)
	}
}

func TestEnterPicksAVideoThenShowsTheCompatibleAudio(t *testing.T) {
	model := newModel(t, Options{MergeFormat: "mp4"})
	analyzedRow(t, model, "https://example.test/a")
	press(model, "right")

	model.fmtCursor = indexOfFormat(model.visible, "137")
	press(model, "enter")

	job := model.jobs[0]
	if job.VideoID != "137" {
		t.Fatalf("video = %q, want 137", job.VideoID)
	}
	if model.stage != stageAudio {
		t.Fatal("picking a video must move the explorer on to the audio streams")
	}
	// opus cannot be merged into mp4 without a re-encode, so only aac is offered.
	if got := ids(model.visible); !sameIDs(got, []string{"140"}) {
		t.Errorf("audio rows = %v, want the compatible aac stream", got)
	}
	if job.AudioID != "140" {
		t.Errorf("audio = %q, want the compatible default 140", job.AudioID)
	}
	if lineContaining(model.View(), "╭─ audio") == "" {
		t.Error("the top table is not titled audio")
	}
}

func TestEnterOnAnAudioRowReplacesTheAudioStream(t *testing.T) {
	model := newModel(t, Options{MergeFormat: "mkv"})
	analyzedRow(t, model, "https://example.test/a")
	press(model, "right")

	model.fmtCursor = indexOfFormat(model.visible, "399")
	press(model, "enter")
	if got := ids(model.visible); !sameIDs(got, []string{"140", "251"}) {
		t.Fatalf("audio rows = %v, want both streams for mkv", got)
	}
	model.fmtCursor = indexOfFormat(model.visible, "140")
	press(model, "enter")

	if model.jobs[0].AudioID != "140" || model.jobs[0].VideoID != "399" {
		t.Errorf("choice = %q/%q", model.jobs[0].VideoID, model.jobs[0].AudioID)
	}
}

func TestEnterOnAMuxedRowClearsTheAudioChoice(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")
	press(model, "right")
	press(model, "tab")
	if model.area != areaMuxed {
		t.Fatalf("tab did not reach the muxed table, area = %v", model.area)
	}

	model.muxedCursor = indexOfFormat(model.muxed, "18")
	press(model, "enter")
	if model.jobs[0].VideoID != "18" || model.jobs[0].AudioID != "" {
		t.Errorf("choice = %q/%q, want the muxed stream alone",
			model.jobs[0].VideoID, model.jobs[0].AudioID)
	}
}

func TestEscapeFromAudioGoesBackToVideos(t *testing.T) {
	model := newModel(t, Options{MergeFormat: "mkv"})
	analyzedRow(t, model, "https://example.test/a")
	press(model, "right")
	model.fmtCursor = indexOfFormat(model.visible, "399")
	press(model, "enter")

	press(model, "esc")
	if model.stage != stageVideo {
		t.Fatal("esc should return to the video list")
	}
	if model.area != areaFormats {
		t.Errorf("esc left the explorer, area = %v", model.area)
	}
	if model.visible[model.fmtCursor].ID != "399" {
		t.Errorf("cursor = %q, want the chosen video", model.visible[model.fmtCursor].ID)
	}
	press(model, "esc")
	if model.area != areaQueue {
		t.Errorf("a second esc should return to the queue, area = %v", model.area)
	}
}

func TestVAKeysNoLongerPickStreams(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")
	press(model, "right")
	video, audio := model.jobs[0].VideoID, model.jobs[0].AudioID

	model.fmtCursor = indexOfFormat(model.visible, "399")
	press(model, "v")
	press(model, "a")
	if model.jobs[0].VideoID != video || model.jobs[0].AudioID != audio {
		t.Errorf("v/a changed the selection to %q/%q; picking is Enter-only",
			model.jobs[0].VideoID, model.jobs[0].AudioID)
	}
}

func TestMuxedTableDisappearsWithoutMuxedFormats(t *testing.T) {
	progressive := fixtureFormats()[:5] // video + audio, no muxed row
	model := newModel(t, Options{})
	analyzedRowWith(t, model, "https://example.test/a", ytdlp.AnalyzeResult{
		Info:    format.Info{Type: "video", ID: "abc", Title: "A video"},
		Formats: progressive,
	})
	press(model, "right")

	if len(model.muxed) != 0 {
		t.Fatalf("muxed rows = %d, want none", len(model.muxed))
	}
	if lineContaining(model.View(), "╭─ muxed") != "" {
		t.Error("the muxed table is rendered with nothing to show")
	}
	press(model, "tab")
	if model.area != areaFilter {
		t.Errorf("tab should skip the missing muxed table, area = %v", model.area)
	}
}

func TestAudioTableFallsBackWhenNothingIsCompatible(t *testing.T) {
	streams := []format.Fmt{
		{ID: "137", Ext: "mp4", Kind: format.Video, VCodec: "avc1.640028", Height: 1080, Available: true},
		{ID: "251", Ext: "webm", Kind: format.Audio, ACodec: "opus", ABR: 160, TBR: 160, Available: true},
	}
	model := newModel(t, Options{MergeFormat: "mp4"})
	analyzedRowWith(t, model, "https://example.test/a", ytdlp.AnalyzeResult{
		Info:    format.Info{Type: "video", ID: "abc", Title: "A video"},
		Formats: streams,
	})
	press(model, "right")
	press(model, "enter")

	// Nothing merges into mp4 without a re-encode, but the table must not be a
	// dead end: the stream is still offered, flagged with the compatibility note.
	if got := ids(model.visible); !sameIDs(got, []string{"251"}) {
		t.Errorf("audio rows = %v, want the opus stream offered anyway", got)
	}
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
	editor.videoSel = indexOfFormat(editor.video, "399")
	editor.audioSel = indexOfFormat(editor.audio, "140")
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
	model.editor.videoSel = indexOfFormat(model.editor.video, "137")
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
	if !model.showLog {
		t.Error("the log should start open")
	}
	if !strings.Contains(model.View(), "yt-dlp:") {
		t.Errorf("log content missing from the view:\n%s", model.View())
	}
	press(model, "l")
	if model.showLog {
		t.Error("l did not hide the log")
	}
	press(model, "l")
	if !model.showLog {
		t.Error("l did not show the log again")
	}
}

func TestLogHidesItselfOnASmallTerminal(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")
	if !strings.Contains(model.View(), "╭─ log") {
		t.Fatalf("the log should be open at 120x40:\n%s", model.View())
	}

	model.Update(tea.WindowSizeMsg{Width: 70, Height: 22})
	if strings.Contains(model.View(), "╭─ log") {
		t.Error("the log should hide when the terminal has no room for it")
	}
	press(model, "l")
	press(model, "l")
	if strings.Contains(model.View(), "╭─ log") {
		t.Error("toggling the log cannot force it into a terminal that is too small")
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

func TestSubmittingAURLMovesToTheFormatExplorer(t *testing.T) {
	model := newModel(t, Options{})
	model.urlInput.SetValue("https://example.test/a")
	press(model, "enter")
	if model.area != areaFormats {
		t.Errorf("after submitting a URL the explorer should own the keys, area = %v", model.area)
	}
}

func TestTabCyclesThroughEveryPane(t *testing.T) {
	model := newModel(t, Options{})
	if model.area != areaURL {
		t.Fatalf("initial area = %v, want the URL bar", model.area)
	}
	order := []area{areaQueue, areaFormats, areaFilter, areaURL}
	for _, want := range order {
		press(model, "tab")
		if model.area != want {
			t.Fatalf("tab moved to %v, want %v", model.area, want)
		}
	}
}

func TestFormatExplorerArrowNavigation(t *testing.T) {
	model := newModel(t, Options{MergeFormat: "mkv"})
	analyzedRow(t, model, "https://example.test/a")
	press(model, "right") // drill from the queue into the explorer
	if model.area != areaFormats {
		t.Fatalf("area = %v, want the format explorer", model.area)
	}
	if model.fmtCursor != 0 {
		t.Fatalf("cursor = %d, want 0", model.fmtCursor)
	}
	press(model, "down")
	if model.fmtCursor != 1 {
		t.Errorf("down did not move the format cursor: %d", model.fmtCursor)
	}
	press(model, "down")
	press(model, "up")
	if model.fmtCursor != 1 {
		t.Errorf("up did not move the format cursor back: %d", model.fmtCursor)
	}
	press(model, "esc")
	if model.area != areaQueue {
		t.Errorf("esc should leave the explorer, area = %v", model.area)
	}
}

func TestMouseWheelScrollsTheFormatExplorer(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")
	model.View() // fills the hit-test map
	if model.hit.formats.rows == 0 {
		t.Fatal("the format panel was not laid out")
	}
	y := model.hit.formats.top
	model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown, Y: y})
	if model.fmtCursor != len(model.visible)-1 {
		t.Errorf("wheel down did not reach the last video: cursor = %d", model.fmtCursor)
	}
	model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp, Y: y})
	if model.fmtCursor != 0 {
		t.Errorf("wheel up did not scroll back: cursor = %d", model.fmtCursor)
	}
}

func TestMouseClickSelectsAFormatRow(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")
	model.View()
	y := model.hit.formats.top + 1
	model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Y: y})
	if model.area != areaFormats {
		t.Errorf("clicking a format row should focus the explorer, area = %v", model.area)
	}
	if model.fmtCursor != model.hit.formats.start+1 {
		t.Errorf("click landed on cursor %d, want %d", model.fmtCursor, model.hit.formats.start+1)
	}
}

func TestMouseClickSelectsAMuxedRow(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")
	model.View()
	y := model.hit.muxed.top
	model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Y: y})
	if model.area != areaMuxed {
		t.Errorf("clicking the muxed table should focus it, area = %v", model.area)
	}
	if model.muxedCursor != model.hit.muxed.start {
		t.Errorf("cursor = %d, want %d", model.muxedCursor, model.hit.muxed.start)
	}
}

func TestMouseClickFocusesAQueueRow(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")
	analyzedRow(t, model, "https://example.test/b")
	model.focus = 0
	model.refreshDerived()
	model.View()
	y := model.hit.queue.top + 1
	model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Y: y})
	if model.focus != 1 || model.area != areaQueue {
		t.Errorf("click on row 2 left focus=%d area=%v", model.focus, model.area)
	}
}

// logText joins the log entries, which is what the assertions read.
func (m *Model) logText() string {
	parts := make([]string, len(m.logLines))
	for index, entry := range m.logLines {
		parts[index] = entry.text
	}
	return strings.Join(parts, "\n")
}

// lineContaining returns the single rendered line that mentions needle.
func lineContaining(view, needle string) string {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

func TestQueueRowCarriesItsOwnProgressBar(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")
	snapshot := model.jobs[0]
	snapshot.Status = format.Downloading
	snapshot.Percent = 42
	snapshot.Speed = "3.2MiB/s"
	model.Update(managerMsg{job: snapshot})

	view := model.View()
	row := lineContaining(view, "A video")
	if !strings.Contains(row, "42%") || !strings.Contains(row, "░") {
		t.Errorf("the queue row has no progress bar: %q", row)
	}
	if strings.Contains(view, " download 42%") {
		t.Error("the progress bar is still rendered as a separate line")
	}
	if !strings.Contains(lineContaining(view, "saves to"), "3.2MiB/s") {
		t.Error("the focused row should report its speed")
	}
}

func TestEarlyProgressShowsTenthsSoItNeverLooksStuck(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")
	snapshot := model.jobs[0]
	snapshot.Status = format.Downloading
	snapshot.Percent = 0.4
	model.Update(managerMsg{job: snapshot})

	row := lineContaining(model.View(), "A video")
	if !strings.Contains(row, "0.4%") {
		t.Errorf("a fourth of a percent should be visible: %q", row)
	}

	snapshot.Percent = 12.4
	model.Update(managerMsg{job: snapshot})
	row = lineContaining(model.View(), "A video")
	if !strings.Contains(row, "12%") || strings.Contains(row, "12.4%") {
		t.Errorf("past ten percent the cell is whole again: %q", row)
	}
}

// The cell must not change width when it switches from tenths to whole
// numbers, or every row under it would shift at the ten percent mark.
func TestStatusCellKeepsItsWidthAcrossTheTenthsBoundary(t *testing.T) {
	job := *format.NewJob(1, "https://example.test/a")
	job.Status = format.Downloading
	labels := map[float64]string{
		0:    "0.0%",
		9.94: "9.9%",
		9.95: "10%",
		10:   "10%",
		42:   "42%",
		100:  "100%",
	}
	width := 0
	for percent, want := range labels {
		job.Percent = percent
		cell := statusCell(job, 20)
		if !strings.Contains(cell, want) {
			t.Errorf("at %v%% the cell reads %q, want %q in it", percent, cell, want)
		}
		if width == 0 {
			width = utf8.RuneCountInString(cell)
			continue
		}
		if got := utf8.RuneCountInString(cell); got != width {
			t.Errorf("at %v%% the cell is %d wide, want %d: %q", percent, got, width, cell)
		}
	}
}

func TestPanelsAreBorderedAndTheLogIsVertical(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")

	view := model.View()
	queueBox := lineContaining(view, "╭─ queue")
	if queueBox == "" {
		t.Fatalf("no queue box:\n%s", view)
	}
	if !strings.Contains(queueBox, "╭─ log") {
		t.Errorf("the log is not a vertical panel beside the queue: %q", queueBox)
	}
	for _, want := range []string{"╭─ videos", "╭─ muxed", "╮", "╯", "│"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q", want)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if width := ansi.StringWidth(line); width > 120 {
			t.Errorf("line wider than the terminal (%d): %q", width, line)
		}
	}
	if model.bodyWidth >= model.width {
		t.Errorf("bodyWidth = %d, the log did not take a column", model.bodyWidth)
	}
}

func TestQueueSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "queue.json")
	first := newModel(t, Options{StatePath: path})
	analyzedRow(t, first, "https://example.test/a")
	first.jobs[0].VideoID, first.jobs[0].AudioID = "399", "140"
	first.jobs[0].Title = "A video"
	first.markDirty()
	first.flushQueue()

	second := newModel(t, Options{StatePath: path})
	if len(second.jobs) != 1 {
		t.Fatalf("restored rows = %d, want 1", len(second.jobs))
	}
	job := second.jobs[0]
	if job.URL != "https://example.test/a" || job.Title != "A video" {
		t.Errorf("row = %+v", job)
	}
	if job.VideoID != "399" || job.AudioID != "140" {
		t.Errorf("selection = %q/%q", job.VideoID, job.AudioID)
	}
	if len(job.Formats) != len(fixtureFormats()) {
		t.Errorf("formats = %d, want %d", len(job.Formats), len(fixtureFormats()))
	}
	if !strings.Contains(second.View(), "A video") {
		t.Error("the restored row is not rendered")
	}
}

func TestRestoredDownloadIsQueuedAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	interrupted := []format.Job{{
		UID: 5, URL: "https://example.test/a", Status: format.Downloading, Percent: 40,
	}}
	if err := store.New(path).Save(interrupted); err != nil {
		t.Fatalf("seed: %v", err)
	}

	model := newModel(t, Options{
		StatePath: path,
		Spawn: func(ctx context.Context, cmd, env []string) (ytdlp.Proc, error) {
			return &yieldingProc{}, nil
		},
	})
	if len(model.jobs) != 1 {
		t.Fatalf("restored rows = %d, want 1", len(model.jobs))
	}
	if model.jobs[0].Status != format.Loaded {
		t.Errorf("an interrupted download must come back queued, status = %q", model.jobs[0].Status)
	}
	if !strings.Contains(model.logText(), "resuming 1") {
		t.Errorf("the interrupted download was not re-queued: %v", model.logLines)
	}
	model.manager.Wait()
}

func TestCorruptQueueFileDoesNotBlockStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	if err := os.WriteFile(path, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	model := newModel(t, Options{StatePath: path})
	if len(model.jobs) != 0 {
		t.Errorf("rows = %d, want none", len(model.jobs))
	}
	if !strings.Contains(model.logText(), "unreadable") {
		t.Errorf("no warning in the log: %v", model.logLines)
	}
}

func TestQuittingFlushesTheQueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	model := newModel(t, Options{StatePath: path})
	model.addURL("https://example.test/a")
	press(model, "q")
	if !model.quitting {
		t.Fatal("q did not quit")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the queue was not flushed on quit: %v", err)
	}
}

func TestExplorerStillHandlesTheGlobalKeys(t *testing.T) {
	model := newModel(t, Options{})
	analyzedRow(t, model, "https://example.test/a")
	press(model, "right") // the explorer owns the keys now
	press(model, "c")
	if !model.compatOnly {
		t.Error("c no longer toggles compatibility while the explorer has focus")
	}
	wasLog := model.showLog
	press(model, "l")
	if model.showLog == wasLog {
		t.Error("l no longer toggles the log while the explorer has focus")
	}
}

func TestLayoutFitsEveryTerminalSize(t *testing.T) {
	sizes := [][2]int{{70, 22}, {76, 24}, {84, 26}, {100, 30}, {120, 40}, {160, 46}}
	for _, size := range sizes {
		for _, log := range []bool{false, true} {
			model := newModel(t, Options{MergeFormat: "mkv"})
			model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			analyzedRow(t, model, "https://example.test/a")
			model.showLog = log
			lines := strings.Split(model.View(), "\n")
			if len(lines) != size[1] {
				t.Errorf("%dx%d log=%v: %d lines", size[0], size[1], log, len(lines))
			}
			for _, line := range lines {
				if width := ansi.StringWidth(line); width > size[0] {
					t.Errorf("%dx%d log=%v: line is %d wide: %q", size[0], size[1], log, width, line)
				}
			}
		}
	}
}

// linesContaining returns every rendered line that mentions needle.
func linesContaining(view, needle string) []string {
	var out []string
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, needle) {
			out = append(out, line)
		}
	}
	return out
}

func TestVideoAndMuxedTablesDropTheConstantColumns(t *testing.T) {
	model := newModel(t, Options{MergeFormat: "mp4"})
	analyzedRow(t, model, "https://example.test/a")
	model.View()

	headers := linesContaining(model.View(), "sel ")
	if len(headers) < 2 {
		t.Fatalf("expected two table headers, got %d:\n%s", len(headers), model.View())
	}
	video, muxed := headers[0], headers[1]
	for _, unwanted := range []string{"id", "ext", "acodec", "hdr"} {
		if strings.Contains(video, unwanted) {
			t.Errorf("the video table still shows %q: %q", unwanted, video)
		}
	}
	for _, want := range []string{"res", "vcodec", "fps", "size"} {
		if !strings.Contains(video, want) {
			t.Errorf("the video table lost %q: %q", want, video)
		}
	}
	for _, unwanted := range []string{"id", "ext"} {
		if strings.Contains(muxed, unwanted) {
			t.Errorf("the muxed table still shows %q: %q", unwanted, muxed)
		}
	}
	if !strings.Contains(muxed, "acodec") || !strings.Contains(muxed, "vcodec") {
		t.Errorf("the muxed table should keep both codecs: %q", muxed)
	}
}

func TestVideoTableKeepsOnlyTheContainerExtension(t *testing.T) {
	model := newModel(t, Options{MergeFormat: "mp4"})
	analyzedRow(t, model, "https://example.test/a")
	if got := ids(model.visible); !sameIDs(got, []string{"137", "399"}) {
		t.Errorf("mp4 container rows = %v, want only the mp4 videos", got)
	}

	// A container that takes anything keeps every stream.
	webm := newModel(t, Options{MergeFormat: "mkv"})
	analyzedRow(t, webm, "https://example.test/a")
	if got := ids(webm.visible); !sameIDs(got, []string{"137", "399", "247"}) {
		t.Errorf("mkv container rows = %v, want every video", got)
	}
}
