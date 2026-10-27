package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"ytd/internal/format"
)

// editor is the modal that picks the video and audio streams for one row.
type editor struct {
	job       format.Job
	merge     string
	video     []format.Fmt
	audio     []format.Fmt // index 0 is "— none —"
	videoSel  int
	audioSel  int
	showBad   bool
	audioSide bool
}

func newEditor(job format.Job, merge string) *editor {
	ed := &editor{job: job, merge: merge}
	ed.rebuild()

	ed.videoSel = 0
	for index, candidate := range ed.video {
		if candidate.ID == job.VideoID {
			ed.videoSel = index
			break
		}
	}
	ed.audioSel = 0
	for index := 1; index < len(ed.audio); index++ {
		if ed.audio[index].ID == job.AudioID {
			ed.audioSel = index
			break
		}
	}
	return ed
}

func (ed *editor) rebuild() {
	ed.video = ed.video[:0]
	ed.audio = []format.Fmt{{ID: ""}}
	for _, candidate := range ed.job.Formats {
		if !candidate.Available {
			continue
		}
		compatible := ed.showBad || format.IsCompatible(candidate, ed.merge)
		switch candidate.Kind {
		case format.Video, format.Muxed:
			if compatible {
				ed.video = append(ed.video, candidate)
			}
		case format.Audio:
			if compatible {
				ed.audio = append(ed.audio, candidate)
			}
		}
	}
	if ed.videoSel >= len(ed.video) {
		ed.videoSel = max(0, len(ed.video)-1)
	}
	if ed.audioSel >= len(ed.audio) {
		ed.audioSel = max(0, len(ed.audio)-1)
	}
}

func (ed *editor) selectedVideo() *format.Fmt {
	if ed.videoSel >= 0 && ed.videoSel < len(ed.video) {
		return &ed.video[ed.videoSel]
	}
	return nil
}

func (ed *editor) selectedAudio() *format.Fmt {
	if ed.audioSel > 0 && ed.audioSel < len(ed.audio) {
		return &ed.audio[ed.audioSel]
	}
	return nil
}

func (ed *editor) label(candidate format.Fmt) string {
	codec := firstNonEmpty(candidate.VCodecName(), candidate.ACodecName(), "?")
	label := fmt.Sprintf("%s · %s · %s · %s",
		candidate.Resolution(), codec, format.HumanBitrate(candidate.TBR), format.HumanSize(candidate.Size))
	if note := format.CompatibilityNote(candidate, ed.merge); note != "" {
		label += "  ⚠ " + note
	}
	return label
}

func (m *Model) handleEditorKey(msg tea.KeyMsg) tea.Cmd {
	ed := m.editor
	switch msg.String() {
	case "esc":
		m.editor = nil
	case "enter":
		m.applyEditor()
		m.editor = nil
	case "tab":
		ed.audioSide = !ed.audioSide
	case "shift+tab":
		ed.audioSide = !ed.audioSide
	case "i", " ":
		ed.showBad = !ed.showBad
		ed.rebuild()
	case "up", "k":
		if ed.audioSide {
			ed.audioSel = clamp(ed.audioSel-1, 0, len(ed.audio)-1)
		} else {
			ed.videoSel = clamp(ed.videoSel-1, 0, len(ed.video)-1)
		}
	case "down", "j":
		if ed.audioSide {
			ed.audioSel = clamp(ed.audioSel+1, 0, len(ed.audio)-1)
		} else {
			ed.videoSel = clamp(ed.videoSel+1, 0, len(ed.video)-1)
		}
	}
	return nil
}

func (m *Model) applyEditor() {
	ed := m.editor
	job := m.jobByUID(ed.job.UID)
	if job == nil {
		return
	}
	job.VideoID = ""
	if candidate := ed.selectedVideo(); candidate != nil {
		job.VideoID = candidate.ID
	}
	job.AudioID = ""
	if candidate := ed.selectedAudio(); candidate != nil {
		job.AudioID = candidate.ID
	}
	if video := job.Video(); video != nil && video.Kind == format.Muxed {
		job.AudioID = ""
	}
	m.refreshDerived()
}

func (m *Model) editorView() string {
	ed := m.editor
	width := min(m.width-6, 76)
	lines := []string{
		titleStyle.Render(truncate(ed.job.Label(60), width)),
		dimStyle.Render("container: " + firstNonEmpty(ed.merge, "yt-dlp default")),
		"",
	}
	side := func(name string, options []format.Fmt, cursor int, active bool) []string {
		header := name
		if active {
			header = accentStyle.Render(name)
		}
		out := []string{header}
		limit := 10
		start := 0
		if cursor > limit-3 {
			start = cursor - limit + 3
		}
		for index := start; index < len(options) && index < start+limit; index++ {
			line := "  " + truncate(ed.label(options[index]), width-4)
			if index == cursor {
				line = selectedStyle.Render("› " + truncate(ed.label(options[index]), width-4))
			}
			out = append(out, line)
		}
		if len(options) == 0 {
			out = append(out, dimStyle.Render("  (none)"))
		}
		return out
	}
	lines = append(lines, side("video", ed.video, ed.videoSel, !ed.audioSide)...)
	lines = append(lines, "")
	lines = append(lines, side("audio", ed.audio, ed.audioSel, ed.audioSide)...)

	note := dimStyle.Render("compatible with the merge container")
	for _, candidate := range []*format.Fmt{ed.selectedVideo(), ed.selectedAudio()} {
		if candidate == nil {
			continue
		}
		if problem := format.CompatibilityNote(*candidate, ed.merge); problem != "" {
			note = warnStyle.Render("⚠ " + problem + " — ffmpeg will re-encode")
		}
	}
	lines = append(lines, "", note)
	if video := ed.selectedVideo(); video != nil && video.Kind == format.Muxed {
		lines = append(lines, dimStyle.Render("muxed stream — audio comes inside the video file"))
	}
	lines = append(lines, dimStyle.Render(fmt.Sprintf(
		"show incompatible: %v  (i) · tab switch · ↑↓ move · Enter apply · Esc cancel", ed.showBad)))

	box := lipglossBox(strings.Join(lines, "\n"), width)
	return placeCenter(box, m.width, m.height)
}
