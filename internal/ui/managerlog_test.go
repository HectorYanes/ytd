package ui

import (
	"context"
	"strings"
	"testing"

	"ytd/internal/format"
	"ytd/internal/ytdlp"
)

// scriptedProc replays a fixed yt-dlp transcript so the manager test never
// touches the network.
type scriptedProc struct{ reader *strings.Reader }

func (p *scriptedProc) Read(buffer []byte) (int, error) { return p.reader.Read(buffer) }
func (p *scriptedProc) Wait() error                     { return nil }
func (p *scriptedProc) Kill() error                     { return nil }

func scripted(output ...string) ytdlp.SpawnFunc {
	return func(context.Context, []string, []string) (ytdlp.Proc, error) {
		return &scriptedProc{reader: strings.NewReader(strings.Join(output, "\n") + "\n")}, nil
	}
}

// TestManagerChatterReachesTheLog is the regression for a download that sat at
// 0%: yt-dlp's own lines (extraction, retries, merging) were swallowed by the
// manager, so an idle transfer had no explanation on screen. Everything runs on
// the test goroutine, so the log is read only after it is complete.
func TestManagerChatterReachesTheLog(t *testing.T) {
	model := newModel(t, Options{Spawn: scripted(
		"[youtube] Extracting URL: https://example.test/a",
		"[download] Got error: Network is unreachable. Retrying (1/10)...",
		"[download]  42.0% of 1.00MiB at 1.00MiB/s ETA 00:01",
		"[Merger] Merging formats into \"/out/a.mp4\"",
	)})

	model.manager.Submit(*format.NewJob(1, "https://example.test/a"))
	model.manager.Wait()

	// One message per forwarded line: the extraction, the retry and the merge.
	for range 3 {
		msg := model.waitLog()()
		if msg == nil {
			t.Fatal("the manager forwarded fewer lines than expected")
		}
		model.Update(msg)
	}

	joined := logText(model)
	for _, want := range []string{
		"#1 [youtube] Extracting URL",
		"#1 [download] Got error: Network is unreachable",
		"#1 [Merger] Merging formats",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the log never saw %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "% of") {
		t.Errorf("a progress line leaked into the log:\n%s", joined)
	}
	if !strings.Contains(model.View(), "#1 [download] Got error") {
		t.Error("the log panel does not show what yt-dlp reported")
	}
}

func TestWaitLogTagsTheLineWithItsRow(t *testing.T) {
	model := newModel(t, Options{})
	model.logs <- managerLog{uid: 7, line: "[download] Destination: /out/a.mp4"}

	msg := model.waitLog()()
	if _, ok := msg.(managerLog); !ok {
		t.Fatalf("waitLog returned %T", msg)
	}
	model.Update(msg)

	if !strings.Contains(logText(model), "#7 [download] Destination") {
		t.Errorf("the log lost the row tag:\n%s", logText(model))
	}
}

func logText(model *Model) string {
	lines := make([]string, 0, len(model.logLines))
	for _, entry := range model.logLines {
		lines = append(lines, entry.text)
	}
	return strings.Join(lines, "\n")
}
