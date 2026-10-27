package ui

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TestProgramRunsAgainstYouTube drives the real program headlessly: the TUI
// starts, analyzes the URL it was given and quits on q. It is opt-in because it
// needs the network and a working yt-dlp.
func TestProgramRunsAgainstYouTube(t *testing.T) {
	if os.Getenv("YTD_E2E") == "" {
		t.Skip("set YTD_E2E=1 to drive the real program")
	}
	model := New(Options{StartURLs: []string{"https://www.youtube.com/watch?v=aqz-KE-bpKQ"}})
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
