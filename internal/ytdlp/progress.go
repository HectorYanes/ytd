package ytdlp

import (
	"regexp"
	"strconv"
	"strings"
)

// Progress is one parsed `[download]` line from yt-dlp.
type Progress struct {
	Percent float64
	Size    string
	Speed   string
	ETA     string
}

var progressPattern = regexp.MustCompile(
	`^\[download\]\s+(\d+(?:\.\d+)?)%\s+of\s+~?\s*(\S+)` +
		`(?:\s+in\s+(\S+))?\s+at\s+(\S+)(?:\s+ETA\s+(\S+))?`,
)

// ParseProgress extracts percent, size, speed and ETA from a yt-dlp line.
// It returns nil for anything that is not a progress line.
func ParseProgress(line string) *Progress {
	match := progressPattern.FindStringSubmatch(strings.TrimSpace(line))
	if match == nil {
		return nil
	}
	percent, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return nil
	}
	return &Progress{
		Percent: percent,
		Size:    match[2],
		Speed:   match[4],
		ETA:     match[5],
	}
}
