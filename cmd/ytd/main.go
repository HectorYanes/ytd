// Command ytd is a queue TUI for yt-dlp.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"ytd/internal/ui"
	"ytd/internal/ytdlp"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "0.2.0-go.1"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	flags := flag.NewFlagSet("ytd", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: ytd [options] [URL ...]")
		fmt.Fprintln(os.Stderr, "\nQueue TUI for yt-dlp.")
		flags.PrintDefaults()
	}

	var (
		outdir      = flags.String("o", "", "download directory (default: ~/Downloads)")
		jobs        = flags.Int("j", 3, "parallel downloads (default: 3)")
		showVersion = flags.Bool("version", false, "print the version and exit")
		check       = flags.Bool("check", false, "print environment diagnostics and exit (no TUI)")
		update      = flags.Bool("update", false, "download or refresh the managed yt-dlp and deno")
	)
	flags.StringVar(outdir, "outdir", "", "download directory (default: ~/Downloads)")
	flags.IntVar(jobs, "jobs", 3, "parallel downloads (default: 3)")

	if err := flags.Parse(argv); err != nil {
		return 2
	}

	switch {
	case *showVersion:
		fmt.Printf("ytd %s\n", version)
		return 0
	case *check:
		return runCheck(*outdir)
	case *update:
		return runUpdate()
	}

	if *jobs < 1 {
		*jobs = 1
	}
	model := ui.New(ui.Options{
		OutDir:        *outdir,
		StartURLs:     flags.Args(),
		MaxConcurrent: *jobs,
	})
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ytd: %v\n", err)
		return 1
	}
	return 0
}

func runCheck(outdir string) int {
	if outdir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		outdir = filepath.Join(home, "Downloads")
	}
	report := ytdlp.Inspect(version, outdir)
	fmt.Println(strings.Join(report.Lines(), "\n"))
	if !report.OK() {
		return 1
	}
	return 0
}

func runUpdate() int {
	fmt.Println("refreshing the managed yt-dlp and deno in the cache directory…")
	err := ytdlp.Update(context.Background(), nil, func(line string) {
		fmt.Println(line)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ytd: update failed: %v\n", err)
		return 1
	}
	return 0
}
