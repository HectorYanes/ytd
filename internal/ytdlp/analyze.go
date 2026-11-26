package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"ytd/internal/format"
)

// AnalyzeCommand is the `-J` invocation for one URL.
func AnalyzeCommand(prefix []string, url string, flat bool) []string {
	cmd := append(append([]string{}, prefix...), "-J", "--no-warnings")
	if flat {
		cmd = append(cmd, "--flat-playlist")
	}
	return append(cmd, "--no-playlist", url)
}

// DownloadCommand builds the download invocation. `-f` is only added when the
// row has an explicit choice, so the user's yt-dlp config still decides
// otherwise.
func DownloadCommand(prefix []string, job format.Job, outDir string) []string {
	cmd := append([]string{}, prefix...)
	if selector := job.Selector(); selector != "" {
		cmd = append(cmd, "-f", selector)
	}
	return append(cmd, "-P", "home:"+outDir, "--newline", job.URL)
}

// DecodeInfo parses yt-dlp stdout, turning junk into a useful error instead of
// an empty video.
func DecodeInfo(payload, errText string, code int) (format.Info, []format.Fmt, error) {
	tail := lastLines(errText, 3)
	if strings.TrimSpace(payload) == "" {
		if tail != "" {
			return format.Info{}, nil, fmt.Errorf("%s", tail)
		}
		return format.Info{}, nil, fmt.Errorf("yt-dlp exited with %d", code)
	}
	trimmed := strings.TrimSpace(payload)
	if !strings.HasPrefix(trimmed, "{") {
		if tail != "" {
			return format.Info{}, nil, fmt.Errorf("%s", tail)
		}
		return format.Info{}, nil, fmt.Errorf("yt-dlp produced no info (exit %d)", code)
	}
	var info format.Info
	if err := json.Unmarshal([]byte(trimmed), &info); err != nil {
		return format.Info{}, nil, fmt.Errorf("could not parse yt-dlp JSON output: %w", err)
	}
	return info, format.ParseFormats(info), nil
}

func lastLines(text string, count int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return strings.TrimSpace(strings.Join(lines, " "))
}

// AnalyzeResult is one successful analysis.
type AnalyzeResult struct {
	Info     format.Info
	Formats  []format.Fmt
	Warnings string
}

// Analyze runs yt-dlp for one URL and decodes its JSON.
//
// stdout and stderr stay separate: yt-dlp writes warnings such as "failed to
// read from keyring" while still emitting complete JSON, and merging the two
// streams can splice that text into the middle of the payload.
func Analyze(ctx context.Context, url string, flat bool, timeout time.Duration) (AnalyzeResult, error) {
	resolution, err := Resolve()
	if err != nil {
		return AnalyzeResult{}, err
	}
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	childCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := AnalyzeCommand(resolution.Prefix, url, flat)
	cmd := exec.CommandContext(childCtx, args[0], args[1:]...)
	cmd.Env = ChildEnv(nil)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if childCtx.Err() == context.DeadlineExceeded {
		return AnalyzeResult{}, fmt.Errorf("yt-dlp timed out after %.0fs", timeout.Seconds())
	}

	code := 0
	if runErr != nil {
		code = exitCode(runErr)
		if code < 0 {
			return AnalyzeResult{}, fmt.Errorf("yt-dlp could not start: %w", runErr)
		}
	}

	payload := stdout.String()
	errText := strings.TrimSpace(stderr.String())
	info, formats, decodeErr := DecodeInfo(payload, errText, code)
	if decodeErr != nil {
		return AnalyzeResult{}, decodeErr
	}
	return AnalyzeResult{Info: info, Formats: formats, Warnings: errText}, nil
}
