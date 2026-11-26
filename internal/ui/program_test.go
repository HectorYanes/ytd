package ui

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"ytd/internal/format"
	"ytd/internal/ytdlp"
)

// TestProgramRunsAgainstYouTube drives the real program headlessly: the TUI
// starts, analyzes the URL it was given and quits on q. It is opt-in because it
// needs the network and a working yt-dlp.
func TestProgramRunsAgainstYouTube(t *testing.T) {
	if os.Getenv("YTD_E2E") == "" {
		t.Skip("set YTD_E2E=1 to drive the real program")
	}
	model := New(Options{
		StartURLs: []string{"https://www.youtube.com/watch?v=aqz-KE-bpKQ"},
		StatePath: filepath.Join(t.TempDir(), "queue.json"),
	})
	reader, writer := io.Pipe()
	var screen bytes.Buffer

	program := tea.NewProgram(model,
		tea.WithInput(reader),
		tea.WithOutput(&screen),
		tea.WithoutSignals(),
		tea.WithoutCatchPanics(),
	)
	done := make(chan error, 1)
	go func() { _, err := program.Run(); done <- err }()

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if len(model.jobs) == 1 && model.jobs[0].Analyzed() {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if _, err := writer.Write([]byte("q")); err != nil {
		t.Fatalf("send q: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the program did not quit on q")
	}

	if len(model.jobs) != 1 || !model.jobs[0].Analyzed() {
		t.Fatalf("no analysis after the run: %+v", model.jobs)
	}
	if !strings.Contains(model.jobs[0].Title, "Big Buck Bunny") {
		t.Errorf("title = %q", model.jobs[0].Title)
	}
	if model.jobs[0].VideoID == "" || model.jobs[0].AudioID == "" {
		t.Errorf("selection = %q/%q", model.jobs[0].VideoID, model.jobs[0].AudioID)
	}
	if screen.Len() == 0 {
		t.Error("the program rendered nothing")
	}
}

// TestProgramShowsYtDlpChatterInTheLog is the regression for a download that
// sat at 0%: yt-dlp's own lines (extraction, retries, merging) used to be
// swallowed by the manager, so an idle transfer had no explanation on screen.
func TestProgramShowsYtDlpChatterInTheLog(t *testing.T) {
	model := New(Options{
		Seed:      []format.Job{*format.NewJob(1, "https://example.test/a")},
		StatePath: filepath.Join(t.TempDir(), "queue.json"),
		Spawn: func(ctx context.Context, cmd, env []string) (ytdlp.Proc, error) {
			return &scriptedProc{reader: strings.NewReader(strings.Join([]string{
				"[youtube] Extracting URL: https://example.test/a",
				"[download] Got error: Network is unreachable. Retrying (1/10)...",
				"[download]  42.0% of 1.00MiB at 1.00MiB/s ETA 00:01",
				"[download] 100% of 1.00MiB in 00:00:01 at 1.00MiB/s",
			}, "\n") + "\n")}, nil
		},
	})
	reader, writer := io.Pipe()
	var screen bytes.Buffer
	program := tea.NewProgram(model,
		tea.WithInput(reader),
		tea.WithOutput(&screen),
		tea.WithoutSignals(),
		tea.WithoutCatchPanics(),
	)
	done := make(chan error, 1)
	go func() { _, err := program.Run(); done <- err }()

	if _, err := writer.Write([]byte("\t")); err != nil {
		t.Fatalf("focus the queue: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := writer.Write([]byte("s")); err != nil {
		t.Fatalf("send s: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if logHas(model, "Network is unreachable") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !logHas(model, "Network is unreachable") {
		t.Errorf("yt-dlp's retry line never reached the log: %v", model.logLines)
	}
	if !logHas(model, "Extracting URL") {
		t.Errorf("yt-dlp's extraction line never reached the log: %v", model.logLines)
	}
	for _, entry := range model.logLines {
		if strings.Contains(entry.text, "% of") {
			t.Errorf("a progress line leaked into the log: %q", entry.text)
		}
	}

	if _, err := writer.Write([]byte("\t")); err != nil {
		t.Fatalf("focus the queue: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := writer.Write([]byte("q")); err != nil {
		t.Fatalf("send q: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the program did not quit on q")
	}
}

func logHas(model *Model, want string) bool {
	for _, entry := range model.logLines {
		if strings.Contains(entry.text, want) {
			return true
		}
	}
	return false
}

type scriptedProc struct{ reader *strings.Reader }

func (p *scriptedProc) Read(buffer []byte) (int, error) { return p.reader.Read(buffer) }
func (p *scriptedProc) Wait() error                     { return nil }
func (p *scriptedProc) Kill() error                     { return nil }
