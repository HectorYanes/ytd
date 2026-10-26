package ytdlp

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ytd/internal/format"
)

func TestParseProgressLine(t *testing.T) {
	got := ParseProgress("[download]  45.2% of  1.32GiB at    2.10MiB/s ETA 00:12")
	if got == nil {
		t.Fatal("expected a progress line")
	}
	if got.Percent != 45.2 || got.Size != "1.32GiB" || got.Speed != "2.10MiB/s" || got.ETA != "00:12" {
		t.Errorf("parsed %+v", got)
	}
}

func TestParseProgressFinishedVariant(t *testing.T) {
	got := ParseProgress("[download] 100% of    9.80MiB in 00:00:08 at 1.18MiB/s")
	if got == nil {
		t.Fatal("expected a progress line")
	}
	if got.Percent != 100 || got.Size != "9.80MiB" || got.Speed != "1.18MiB/s" || got.ETA != "" {
		t.Errorf("parsed %+v", got)
	}
}

func TestParseProgressIgnoresOtherLines(t *testing.T) {
	for _, line := range []string{
		"[download] Destination: /tmp/a.mp4",
		`[Merger] Merging formats into "/tmp/a.mp4"`,
		"",
		"ERROR: [youtube] x: Video unavailable",
	} {
		if got := ParseProgress(line); got != nil {
			t.Errorf("ParseProgress(%q) = %+v, want nil", line, got)
		}
	}
}

func TestConfigCandidatesPerPlatform(t *testing.T) {
	home := "/home/u"
	linux := ConfigCandidates("linux", home, map[string]string{})
	if linux[0] != "/home/u/.config/yt-dlp/config" {
		t.Errorf("linux first candidate = %q", linux[0])
	}
	xdg := ConfigCandidates("linux", home, map[string]string{"XDG_CONFIG_HOME": "/xdg"})
	if xdg[0] != "/xdg/yt-dlp/config" {
		t.Errorf("xdg candidate = %q", xdg[0])
	}
	windows := ConfigCandidates("windows", `C:\Users\u`, map[string]string{"APPDATA": `C:\Users\u\AppData\Roaming`})
	if !strings.Contains(windows[0], "yt-dlp") || !strings.HasSuffix(windows[0], "config") {
		t.Errorf("windows candidates = %v", windows)
	}
	if len(windows) < 3 {
		t.Errorf("windows should check APPDATA and the home fallback: %v", windows)
	}
	darwin := ConfigCandidates("darwin", home, map[string]string{})
	if !strings.Contains(darwin[0], "Application Support") {
		t.Errorf("darwin candidate = %q", darwin[0])
	}
}

func TestParseMergeFormatVariants(t *testing.T) {
	cases := map[string]string{
		"--merge-output-format mp4":  "mp4",
		"--merge-output-format=mkv":  "mkv",
		"  --remux-video webm  ":     "webm",
		"--merge-output-format\nMP4": "mp4",
		"--format bv*+ba/b":          "",
		"":                           "",
	}
	for text, want := range cases {
		if got := ParseMergeFormat(text); got != want {
			t.Errorf("ParseMergeFormat(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestDetectMergeFormatReadsConfig(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "yt-dlp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "config")
	if err := os.WriteFile(config, []byte("--embed-metadata\n--merge-output-format mp4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	if got := DetectMergeFormat("linux", home, env); got != "mp4" {
		t.Errorf("merge format = %q, want mp4", got)
	}
	if got := DetectMergeFormat("linux", t.TempDir(), env); got != "" {
		t.Errorf("missing config should yield empty, got %q", got)
	}
}

func TestDecodeInfoRejectsJunk(t *testing.T) {
	if _, _, err := DecodeInfo("null", "ERROR: [youtube] x: This video is unavailable", 1); err == nil ||
		!strings.Contains(err.Error(), "unavailable") {
		t.Errorf("null payload: err = %v, want the error tail", err)
	}
	if _, _, err := DecodeInfo("", "", 2); err == nil || !strings.Contains(err.Error(), "2") {
		t.Errorf("empty payload: err = %v", err)
	}
	if _, _, err := DecodeInfo("not json", "", 0); err == nil {
		t.Error("garbage payload should fail")
	}
	info, formats, err := DecodeInfo(`{"_type":"video","title":"x"}`, "", 0)
	if err != nil || info.Type != "video" || len(formats) != 0 {
		t.Errorf("valid payload: info=%+v formats=%v err=%v", info, formats, err)
	}
}

func TestAnalyzeAndDownloadCommandShapes(t *testing.T) {
	analyze := AnalyzeCommand([]string{"yt-dlp"}, "URL", true)
	if strings.Join(analyze, " ") != "yt-dlp -J --no-warnings --flat-playlist --no-playlist URL" {
		t.Errorf("analyze command = %v", analyze)
	}
	plain := AnalyzeCommand([]string{"yt-dlp"}, "URL", false)
	if strings.Join(plain, " ") != "yt-dlp -J --no-warnings --no-playlist URL" {
		t.Errorf("flat-less command = %v", plain)
	}

	job := format.NewJob(1, "URL")
	job.VideoID, job.AudioID = "299", "140"
	got := DownloadCommand([]string{"yt-dlp"}, *job, "/out")
	if strings.Join(got, " ") != "yt-dlp -f 299+140 -P home:/out --newline URL" {
		t.Errorf("download command = %v", got)
	}
	auto := DownloadCommand([]string{"yt-dlp"}, *format.NewJob(1, "URL"), "/out")
	if strings.Join(auto, " ") != "yt-dlp -P home:/out --newline URL" {
		t.Errorf("auto download command must not pass -f: %v", auto)
	}
}

func TestResolveOrder(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("YTD_YTDLP", "/custom/yt-dlp")
	resolution, err := Resolve()
	if err != nil || resolution.Prefix[0] != "/custom/yt-dlp" ||
		!strings.HasPrefix(resolution.Origin, "YTD_YTDLP=") {
		t.Errorf("override: %+v err=%v", resolution, err)
	}
	t.Setenv("YTD_YTDLP", "")

	fake := filepath.Join(binDir, "yt-dlp")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	resolution, err = Resolve()
	if err != nil || resolution.Prefix[0] != fake || !strings.HasPrefix(resolution.Origin, "PATH") {
		t.Errorf("PATH: %+v err=%v", resolution, err)
	}

	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("PATH", t.TempDir())
	managedDir := filepath.Join(cache, "ytd", "bin")
	if err := os.MkdirAll(managedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(managedDir, YtdlpName())
	if err := os.WriteFile(managed, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolution, err = Resolve()
	if err != nil || resolution.Prefix[0] != managed || !strings.HasPrefix(resolution.Origin, "managed") {
		t.Errorf("managed: %+v err=%v", resolution, err)
	}

	if err := os.Remove(managed); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(); err == nil || !strings.Contains(err.Error(), "yt-dlp not found") {
		t.Errorf("missing everywhere: err = %v", err)
	}
}

func TestJSRuntimePrefersPathThenExtraDirs(t *testing.T) {
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	if got := JSRuntime(nil); got != "" {
		t.Errorf("JSRuntime with empty PATH = %q", got)
	}
	extra := t.TempDir()
	deno := filepath.Join(extra, "deno"+ExeSuffix())
	if err := os.WriteFile(deno, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := JSRuntime([]string{extra}); got != deno {
		t.Errorf("JSRuntime(extra) = %q, want %q", got, deno)
	}
}

func TestChildEnvPrependsManagedBinDir(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	env := ChildEnv([]string{"/extra"})
	var path string
	for _, entry := range env {
		if strings.HasPrefix(entry, "PATH=") {
			path = strings.TrimPrefix(entry, "PATH=")
		}
	}
	want := filepath.Join(cache, "ytd", "bin")
	if !strings.HasPrefix(path, "/extra"+string(os.PathListSeparator)+want) {
		t.Errorf("child PATH = %q, want it to start with /extra and %q", path, want)
	}
}

func TestDoctorRendersMissingPieces(t *testing.T) {
	report := Doctor{Version: "9.9", Ytdlp: "missing", JSRuntime: "", OutDir: "/out"}
	text := strings.Join(report.Lines(), "\n")
	if !strings.Contains(text, "yt-dlp     missing") {
		t.Errorf("missing yt-dlp not reported:\n%s", text)
	}
	if !strings.Contains(text, "MISSING") {
		t.Errorf("missing ffmpeg not reported:\n%s", text)
	}
	if !strings.Contains(text, "js runtime none") {
		t.Errorf("missing runtime not reported:\n%s", text)
	}
	if report.OK() {
		t.Error("report should not be OK without yt-dlp and ffmpeg")
	}
}

// ---------------------------------------------------------------------------
// verified downloads
// ---------------------------------------------------------------------------

func TestDownloadVerifiedAcceptsMatchingChecksum(t *testing.T) {
	payload := []byte("binary")
	sum := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/yt-dlp":
			w.Write(payload)
		case "/SHA2-256SUMS":
			fmt.Fprintf(w, "%s  yt-dlp\n", hex.EncodeToString(sum[:]))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "yt-dlp")
	if err := DownloadVerified(context.Background(), server.Client(),
		server.URL+"/yt-dlp", server.URL+"/SHA2-256SUMS", dest); err != nil {
		t.Fatalf("download: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, payload) {
		t.Errorf("content = %q err=%v", got, err)
	}
}

func TestDownloadVerifiedRejectsWrongChecksum(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/yt-dlp":
			w.Write([]byte("tampered"))
		case "/SHA2-256SUMS":
			fmt.Fprintf(w, "%s  yt-dlp\n", strings.Repeat("0", 64))
		}
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "yt-dlp")
	err := DownloadVerified(context.Background(), server.Client(),
		server.URL+"/yt-dlp", server.URL+"/SHA2-256SUMS", dest)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v, want a checksum failure", err)
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Error("a rejected download must not be left in place")
	}
}

func TestDownloadZipEntryVerifiedExtractsBinary(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create("deno")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("deno-binary")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive.Bytes())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/deno-x86_64-unknown-linux-gnu.zip":
			w.Write(archive.Bytes())
		case "/deno-x86_64-unknown-linux-gnu.zip.sha256sum":
			fmt.Fprintf(w, "%s  deno-x86_64-unknown-linux-gnu.zip\n", hex.EncodeToString(sum[:]))
		}
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "deno")
	if err := DownloadZipEntryVerified(context.Background(), server.Client(),
		server.URL+"/deno-x86_64-unknown-linux-gnu.zip",
		server.URL+"/deno-x86_64-unknown-linux-gnu.zip.sha256sum",
		"deno", dest); err != nil {
		t.Fatalf("download: %v", err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("extracted binary is not executable: %v", info.Mode())
	}
	data, _ := os.ReadFile(dest)
	if string(data) != "deno-binary" {
		t.Errorf("content = %q", data)
	}
}

// ---------------------------------------------------------------------------
// download pool
// ---------------------------------------------------------------------------

type fakeProc struct {
	reader  io.Reader
	waitErr error
}

func (p *fakeProc) Read(buffer []byte) (int, error) { return p.reader.Read(buffer) }
func (p *fakeProc) Wait() error                     { return p.waitErr }
func (p *fakeProc) Kill() error                     { return nil }

func staticSpawn(lines []string, waitErr error) SpawnFunc {
	return func(ctx context.Context, cmd, env []string) (Proc, error) {
		return &fakeProc{reader: strings.NewReader(strings.Join(lines, "\n") + "\n"), waitErr: waitErr}, nil
	}
}

func recorder() (func(format.Job), func() []format.Job) {
	var mu sync.Mutex
	var updates []format.Job
	return func(job format.Job) {
			mu.Lock()
			updates = append(updates, job)
			mu.Unlock()
		}, func() []format.Job {
			mu.Lock()
			defer mu.Unlock()
			out := make([]format.Job, len(updates))
			copy(out, updates)
			return out
		}
}

func TestManagerReportsProgressThenDone(t *testing.T) {
	onUpdate, updates := recorder()
	manager := NewManager(t.TempDir(), 1, onUpdate)
	manager.Prefix = []string{"yt-dlp"}
	manager.Spawn = staticSpawn([]string{
		"[download] Destination: /out/a.mp4",
		"[download]  45.2% of 1.32GiB at 2.10MiB/s ETA 00:12",
		"[download] 100% of 9.80MiB in 00:00:08 at 1.18MiB/s",
	}, nil)

	job := format.NewJob(1, "URL")
	if added := manager.Submit(*job); added != 1 {
		t.Fatalf("added = %d", added)
	}
	manager.Wait()

	final, ok := manager.Snapshot(1)
	if !ok {
		t.Fatal("job missing from the manager")
	}
	if final.Status != format.Done || final.Percent != 100 {
		t.Errorf("final = %+v", final)
	}
	if final.Path != "/out/a.mp4" {
		t.Errorf("path = %q", final.Path)
	}
	seenProgress := false
	for _, update := range updates() {
		if update.Status == format.Downloading && update.Percent == 45.2 {
			seenProgress = true
		}
	}
	if !seenProgress {
		t.Error("no progress update reached the callback")
	}
}

func TestManagerMarksErrors(t *testing.T) {
	manager := NewManager(t.TempDir(), 1, nil)
	manager.Prefix = []string{"yt-dlp"}
	manager.Spawn = staticSpawn([]string{"ERROR: [youtube] x: Video unavailable"}, fmt.Errorf("exit status 1"))

	manager.Submit(*format.NewJob(7, "URL"))
	manager.Wait()

	final, _ := manager.Snapshot(7)
	if final.Status != format.Error {
		t.Errorf("status = %q", final.Status)
	}
	if !strings.Contains(final.Error, "unavailable") {
		t.Errorf("error = %q", final.Error)
	}
}

func TestManagerCapsConcurrency(t *testing.T) {
	var mu sync.Mutex
	live, peak := 0, 0
	spawn := func(ctx context.Context, cmd, env []string) (Proc, error) {
		mu.Lock()
		live++
		if live > peak {
			peak = live
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		live--
		mu.Unlock()
		return &fakeProc{reader: strings.NewReader("[download] 100% of 1MiB at 1MiB/s\n")}, nil
	}
	manager := NewManager(t.TempDir(), 2, nil)
	manager.Prefix = []string{"yt-dlp"}
	manager.Spawn = spawn

	jobs := []format.Job{}
	for uid := 1; uid <= 4; uid++ {
		jobs = append(jobs, *format.NewJob(uid, "URL"))
	}
	if added := manager.Submit(jobs...); added != 4 {
		t.Fatalf("added = %d", added)
	}
	manager.Wait()
	if peak > 2 {
		t.Errorf("peak concurrency = %d, want at most 2", peak)
	}
	if manager.MaxObserved() > 2 {
		t.Errorf("MaxObserved = %d", manager.MaxObserved())
	}
}

func TestManagerDedupesSubmits(t *testing.T) {
	manager := NewManager(t.TempDir(), 1, nil)
	manager.Prefix = []string{"yt-dlp"}
	manager.Spawn = staticSpawn([]string{"[download] 100% of 1MiB at 1MiB/s"}, nil)
	job := format.NewJob(3, "URL")
	if added := manager.Submit(*job, *job); added != 1 {
		t.Errorf("added = %d, want 1", added)
	}
	manager.Wait()
	// Duplicates are skipped only while queued or running; a finished row can
	// be retried, which is how a failed download is re-queued.
	if added := manager.Submit(*job); added != 1 {
		t.Errorf("re-submit after completion = %d, want 1 (retry allowed)", added)
	}
	manager.Wait()
}

func TestManagerCancelMarksCancelled(t *testing.T) {
	manager := NewManager(t.TempDir(), 1, nil)
	manager.Prefix = []string{"yt-dlp"}
	blocked := make(chan struct{})
	manager.Spawn = func(ctx context.Context, cmd, env []string) (Proc, error) {
		return &blockingProc{ctx: ctx, started: blocked}, nil
	}

	manager.Submit(*format.NewJob(9, "URL"))
	<-blocked
	if !manager.Cancel(9) {
		t.Fatal("Cancel should report a running job")
	}
	manager.Wait()

	final, _ := manager.Snapshot(9)
	if final.Status != format.Cancelled {
		t.Errorf("status = %q, want cancelado", final.Status)
	}
	if manager.Cancel(9) {
		t.Error("Cancel on a finished job should be false")
	}
}

type blockingProc struct {
	ctx     context.Context
	started chan struct{}
	once    sync.Once
}

func (p *blockingProc) Read(buffer []byte) (int, error) {
	p.once.Do(func() { close(p.started) })
	<-p.ctx.Done()
	return 0, p.ctx.Err()
}

func (p *blockingProc) Wait() error { return p.ctx.Err() }
func (p *blockingProc) Kill() error { return nil }

// ---------------------------------------------------------------------------
// real network probe (opt-in: YTD_E2E=1)
// ---------------------------------------------------------------------------

func TestAnalyzeRealURL(t *testing.T) {
	if os.Getenv("YTD_E2E") == "" {
		t.Skip("set YTD_E2E=1 to run the real yt-dlp probe")
	}
	if _, err := Resolve(); err != nil {
		t.Skipf("no yt-dlp available: %v", err)
	}
	result, err := Analyze(context.Background(),
		"https://www.youtube.com/watch?v=aqz-KE-bpKQ", true, 120*time.Second)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(result.Formats) == 0 {
		t.Fatal("no formats")
	}
	videoID, audioID := format.DefaultSelection(result.Formats, "")
	t.Logf("title=%q formats=%d default=%s+%s", result.Info.Title, len(result.Formats), videoID, audioID)
	if result.Info.Title == "" || videoID == "" {
		t.Errorf("unexpected result: title=%q formats=%d default=%q+%q infoType=%q",
			result.Info.Title, len(result.Formats), videoID, audioID, result.Info.Type)
	}
}
