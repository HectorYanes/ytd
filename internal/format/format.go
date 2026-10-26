// Package format holds the data model: formats, filtering, availability and
// container compatibility. It is pure logic — no TUI, no subprocesses.
package format

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Kind classifies one yt-dlp stream.
type Kind string

const (
	Muxed Kind = "muxed"
	Video Kind = "video"
	Audio Kind = "audio"
	Image Kind = "image"
)

const hlsProtocol = "m3u8"

var codecAliases = []struct{ needle, name string }{
	{"avc1", "h264"}, {"avc3", "h264"}, {"hev1", "h265"}, {"hvc1", "h265"},
	{"av01", "av1"}, {"vp09", "vp9"}, {"vp9", "vp9"}, {"vp08", "vp8"},
	{"mp4a", "aac"}, {"opus", "opus"}, {"vorbis", "vorbis"},
	{"ec-3", "eac3"}, {"ac-3", "ac3"}, {"dts", "dts"}, {"flac", "flac"},
}

// CodecName maps a yt-dlp codec string to a short human name.
func CodecName(raw string) string {
	if raw == "" || raw == "none" {
		return ""
	}
	low := strings.ToLower(raw)
	for _, alias := range codecAliases {
		if strings.HasPrefix(low, alias.needle) || strings.Contains(low, alias.needle) {
			return alias.name
		}
	}
	return strings.Split(low, ".")[0]
}

// HumanSize renders a byte count. A nil size means "unknown".
func HumanSize(size *int64) string {
	if size == nil {
		return "—"
	}
	if *size < 1024 {
		return fmt.Sprintf("%d B", *size)
	}
	value := float64(*size)
	for _, unit := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= 1024
		if value < 1024 {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return fmt.Sprintf("%.1f PiB", value)
}

// HumanDuration renders seconds as m:ss or h:mm:ss.
func HumanDuration(seconds float64) string {
	if seconds == 0 {
		return "—"
	}
	total := int64(seconds)
	hours := total / 3600
	rest := total % 3600
	minutes := rest / 60
	secs := rest % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, secs)
	}
	return fmt.Sprintf("%d:%02d", minutes, secs)
}

// HumanBitrate renders kbps as 1.5M or 129k.
func HumanBitrate(kbps float64) string {
	if kbps == 0 {
		return "—"
	}
	if kbps >= 1000 {
		return fmt.Sprintf("%.1fM", kbps/1000)
	}
	return fmt.Sprintf("%.0fk", kbps)
}

// Fmt is one row of yt-dlp's format list.
type Fmt struct {
	ID       string
	Ext      string
	VCodec   string
	ACodec   string
	Height   int
	Width    int
	FPS      float64
	TBR      float64
	ABR      float64
	Size     *int64
	Note     string
	HDR      string
	Language string
	Protocol string
	Kind     Kind
	// Available is false for storyboards and other non-downloadable rows.
	Available bool
}

// VCodecName is the short name of the video codec, empty when absent.
func (f Fmt) VCodecName() string { return CodecName(f.VCodec) }

// ACodecName is the short name of the audio codec, empty when absent.
func (f Fmt) ACodecName() string { return CodecName(f.ACodec) }

// IsHLS reports whether the stream is served as an HLS playlist.
func (f Fmt) IsHLS() bool { return strings.Contains(f.Protocol, hlsProtocol) }

// Resolution is the label shown in the explorer: 1080p60, audio, image, —.
func (f Fmt) Resolution() string {
	switch f.Kind {
	case Audio:
		return "audio"
	case Image:
		if f.Height > 0 {
			return fmt.Sprintf("%dp", f.Height)
		}
		return "image"
	}
	if f.Height == 0 {
		return "—"
	}
	if f.FPS > 30 {
		return fmt.Sprintf("%dp%d", f.Height, int(f.FPS))
	}
	return fmt.Sprintf("%dp", f.Height)
}

// HDRLabel is the dynamic range tag, empty for SDR or unknown.
func (f Fmt) HDRLabel() string {
	if f.HDR == "" || f.HDR == "SDR" {
		return ""
	}
	return f.HDR
}

// Haystack is the lowercased text the free-text filter matches against.
func (f Fmt) Haystack() string {
	parts := []string{
		f.ID, f.Ext, string(f.Kind), f.Resolution(),
		f.VCodecName(), f.ACodecName(), f.HDRLabel(), f.Note, f.Language,
	}
	if f.Height > 0 {
		parts = append(parts, fmt.Sprintf("%d", f.Height))
	}
	return strings.ToLower(strings.Join(parts, " "))
}

// rawFormat mirrors the subset of yt-dlp's JSON we consume.
type rawFormat struct {
	FormatID       any      `json:"format_id"`
	Ext            string   `json:"ext"`
	VCodec         string   `json:"vcodec"`
	ACodec         string   `json:"acodec"`
	Height         int      `json:"height"`
	Width          int      `json:"width"`
	FPS            float64  `json:"fps"`
	TBR            float64  `json:"tbr"`
	ABR            float64  `json:"abr"`
	Filesize       *float64 `json:"filesize"`
	FilesizeApprox *float64 `json:"filesize_approx"`
	FormatNote     string   `json:"format_note"`
	DynamicRange   string   `json:"dynamic_range"`
	Language       string   `json:"language"`
	Protocol       string   `json:"protocol"`
}

// Info is the part of `yt-dlp -J` output the app uses.
type Info struct {
	Type       string      `json:"_type"`
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Uploader   string      `json:"uploader"`
	Duration   float64     `json:"duration"`
	IsLive     bool        `json:"is_live"`
	WebpageURL string      `json:"webpage_url"`
	Entries    []Info      `json:"entries"`
	Formats    []rawFormat `json:"formats"`
}

func jsonString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		if typed == math.Trunc(typed) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return fmt.Sprintf("%v", typed)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", typed)
	}
}

// ParseFormats turns an info payload into format rows, preserving order.
func ParseFormats(info Info) []Fmt {
	out := make([]Fmt, 0, len(info.Formats))
	for _, raw := range info.Formats {
		id := jsonString(raw.FormatID)
		if id == "" {
			continue
		}
		kind, available := classify(raw)
		var size *int64
		switch {
		case raw.Filesize != nil:
			bytes := int64(math.Round(*raw.Filesize))
			size = &bytes
		case raw.FilesizeApprox != nil:
			bytes := int64(math.Round(*raw.FilesizeApprox))
			size = &bytes
		}
		ext := raw.Ext
		if ext == "" {
			ext = "?"
		}
		out = append(out, Fmt{
			ID:        id,
			Ext:       ext,
			VCodec:    raw.VCodec,
			ACodec:    raw.ACodec,
			Height:    raw.Height,
			Width:     raw.Width,
			FPS:       raw.FPS,
			TBR:       raw.TBR,
			ABR:       raw.ABR,
			Size:      size,
			Note:      raw.FormatNote,
			HDR:       raw.DynamicRange,
			Language:  raw.Language,
			Protocol:  raw.Protocol,
			Kind:      kind,
			Available: available,
		})
	}
	return out
}

// UnmarshalInfo decodes `yt-dlp -J` output and its formats in one step.
func UnmarshalInfo(payload []byte) (Info, []Fmt, error) {
	var info Info
	if err := json.Unmarshal(payload, &info); err != nil {
		return Info{}, nil, err
	}
	return info, ParseFormats(info), nil
}

func classify(raw rawFormat) (Kind, bool) {
	hasVideo := raw.VCodec != "" && raw.VCodec != "none"
	hasAudio := raw.ACodec != "" && raw.ACodec != "none"
	if raw.Ext == "mhtml" || raw.Ext == "mhtml_list" || (!hasVideo && !hasAudio) {
		return Image, false
	}
	if hasVideo && hasAudio {
		return Muxed, true
	}
	if hasVideo {
		return Video, true
	}
	return Audio, true
}

// Filters narrow the explorer table. The zero value shows everything that is
// downloadable, including HLS variants and hiding storyboards.
type Filters struct {
	Text         string
	Kind         string // "all", "muxed", "video", "audio" or "image"
	Container    string // "all" or an extension such as "mp4"
	OnlyWithSize bool
	HideHLS      bool
	ShowImages   bool
}

// ApplyFilters keeps the rows the user asked for, in their original order.
func ApplyFilters(fmts []Fmt, f Filters) []Fmt {
	needle := strings.ToLower(strings.TrimSpace(f.Text))
	out := make([]Fmt, 0, len(fmts))
	for _, item := range fmts {
		if !item.Available && !f.ShowImages {
			continue
		}
		switch {
		case f.Kind == string(Image):
			if item.Kind != Image {
				continue
			}
		case f.Kind != "" && f.Kind != "all":
			if string(item.Kind) != f.Kind {
				continue
			}
		}
		if f.Container != "" && f.Container != "all" && item.Ext != f.Container {
			continue
		}
		if f.OnlyWithSize && (item.Size == nil || *item.Size == 0) {
			continue
		}
		if f.HideHLS && item.IsHLS() {
			continue
		}
		if needle != "" && !strings.Contains(item.Haystack(), needle) {
			continue
		}
		out = append(out, item)
	}
	return out
}

var videoCodecsByContainer = map[string]map[string]bool{
	"mp4":  {"h264": true, "h265": true, "av1": true, "mpeg4": true},
	"webm": {"vp8": true, "vp9": true, "av1": true},
}

var audioCodecsByContainer = map[string]map[string]bool{
	"mp4":  {"aac": true, "mp3": true, "ac3": true, "eac3": true, "alac": true},
	"m4a":  {"aac": true, "mp3": true, "alac": true},
	"webm": {"opus": true, "vorbis": true},
}

// ContainerFamily normalizes a merge container to the family its codec rules
// use, so "mov" is judged as mp4 and "matroska" as mkv.
func ContainerFamily(mergeFormat string) string {
	name := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(mergeFormat)), ".")
	switch name {
	case "mov":
		return "mp4"
	case "matroska":
		return "mkv"
	}
	return name
}

// IsCompatible reports whether a stream can be muxed into the configured
// container without a re-encode.
func IsCompatible(f Fmt, mergeFormat string) bool {
	if (f.Kind != Video && f.Kind != Audio) || mergeFormat == "" {
		return true
	}
	family := ContainerFamily(mergeFormat)
	if family == "" || family == "mkv" {
		return true
	}
	if f.Kind == Video {
		allowed, ok := videoCodecsByContainer[family]
		if !ok {
			allowed = videoCodecsByContainer["mp4"]
		}
		codec := f.VCodecName()
		return codec == "" || allowed[codec]
	}
	allowed, ok := audioCodecsByContainer[family]
	if !ok {
		allowed = audioCodecsByContainer["mp4"]
	}
	codec := f.ACodecName()
	return codec == "" || allowed[codec]
}

// CompatibilityNote explains an incompatible stream in the note column.
func CompatibilityNote(f Fmt, mergeFormat string) string {
	if IsCompatible(f, mergeFormat) {
		return ""
	}
	codec := f.VCodecName()
	if f.Kind == Audio {
		codec = f.ACodecName()
	}
	if codec == "" {
		codec = "?"
	}
	return fmt.Sprintf("%s not compatible with %s", codec, ContainerFamily(mergeFormat))
}

// LineKind lets the UI style each line of the availability report.
type LineKind string

const (
	LineResolution LineKind = "resolution"
	LineAudio      LineKind = "audio"
	LineWarning    LineKind = "warning"
	LineInfo       LineKind = "info"
)

// ReportLine is one styled line of the availability report.
type ReportLine struct {
	Kind LineKind
	Text string
}

// AvailabilityReport summarizes what is actually downloadable, grouped by
// height, plus the warnings worth reading before choosing.
func AvailabilityReport(fmts []Fmt, mergeFormat string) []ReportLine {
	var lines []ReportLine
	videos := make([]Fmt, 0, len(fmts))
	audios := make([]Fmt, 0, len(fmts))
	for _, f := range fmts {
		if f.Available && f.Height > 0 && (f.Kind == Video || f.Kind == Muxed) {
			videos = append(videos, f)
		}
		if f.Kind == Audio {
			audios = append(audios, f)
		}
	}

	heights := map[int]bool{}
	for _, f := range videos {
		heights[f.Height] = true
	}
	ordered := make([]int, 0, len(heights))
	for height := range heights {
		ordered = append(ordered, height)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ordered)))

	for _, height := range ordered {
		group := make([]Fmt, 0, len(videos))
		for _, f := range videos {
			if f.Height == height {
				group = append(group, f)
			}
		}
		fps := 0.0
		codecSet := map[string]bool{}
		hdrSet := map[string]bool{}
		var muxed, progressive []Fmt
		for _, f := range group {
			fps = math.Max(fps, f.FPS)
			if codec := f.VCodecName(); codec != "" {
				codecSet[codec] = true
			}
			if label := f.HDRLabel(); label != "" {
				hdrSet[label] = true
			}
			if f.Kind == Muxed {
				muxed = append(muxed, f)
				if !f.IsHLS() {
					progressive = append(progressive, f)
				}
			}
		}
		heightLabel := fmt.Sprintf("%dp", height)
		if fps > 30 {
			heightLabel = fmt.Sprintf("%dp%d", height, int(fps))
		}
		bits := []string{heightLabel}
		if codes := sortedKeys(codecSet); len(codes) > 0 {
			bits = append(bits, strings.Join(codes, ", "))
		} else {
			bits = append(bits, "—")
		}
		if hdrs := sortedKeys(hdrSet); len(hdrs) > 0 {
			bits = append(bits, strings.Join(hdrs, "/"))
		}
		switch {
		case len(progressive) > 0:
			bits = append(bits, "muxed ready")
		case len(muxed) > 0:
			bits = append(bits, "muxed via HLS")
		default:
			bits = append(bits, "video-only")
		}
		lines = append(lines, ReportLine{Kind: LineResolution, Text: strings.Join(bits, "  ·  ")})
	}

	if len(audios) > 0 {
		best := map[string]Fmt{}
		for _, a := range audios {
			codec := a.ACodecName()
			if codec == "" {
				continue
			}
			current, seen := best[codec]
			if !seen || audioRank(a) > audioRank(current) {
				best[codec] = a
			}
		}
		codecs := make([]string, 0, len(best))
		for codec := range best {
			codecs = append(codecs, codec)
		}
		sort.Strings(codecs)
		parts := make([]string, 0, len(codecs))
		for _, codec := range codecs {
			item := best[codec]
			rate := item.ABR
			if rate == 0 {
				rate = item.TBR
			}
			parts = append(parts, codec+" "+HumanBitrate(rate))
		}
		if len(parts) > 0 {
			lines = append(lines, ReportLine{
				Kind: LineAudio,
				Text: "audio (best per codec)  ·  " + strings.Join(parts, ", "),
			})
		}
	}

	if len(ordered) > 0 {
		top := ordered[0]
		topProgressive := false
		for _, f := range videos {
			if f.Height == top && f.Kind == Muxed && !f.IsHLS() {
				topProgressive = true
			}
		}
		if !topProgressive {
			lines = append(lines, ReportLine{
				Kind: LineWarning,
				Text: fmt.Sprintf("⚠ %dp is video-only: it needs a separate audio stream (yt-dlp merges them with ffmpeg).", top),
			})
		}
	}
	if mergeFormat == "mp4" {
		for _, a := range audios {
			if a.ACodecName() == "opus" {
				lines = append(lines, ReportLine{
					Kind: LineWarning,
					Text: "⚠ best audio is opus and your merge container is mp4: QuickTime and older players may not read it.",
				})
				break
			}
		}
	}

	var missing []string
	for _, f := range fmts {
		if f.Available && (f.Size == nil || *f.Size == 0) {
			missing = append(missing, f.ID)
		}
	}
	if len(missing) > 0 {
		shown := missing
		suffix := ""
		if len(shown) > 4 {
			shown = shown[:4]
			suffix = "…"
		}
		lines = append(lines, ReportLine{
			Kind: LineInfo,
			Text: fmt.Sprintf("· %d format(s) report no size (%s%s): size is known after the first bytes.",
				len(missing), strings.Join(shown, ", "), suffix),
		})
	}
	return lines
}

func audioRank(f Fmt) float64 {
	if f.ABR != 0 {
		return f.ABR
	}
	return f.TBR
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// BuildFormatSelector is the `-f` value: video+audio, or whichever exists.
func BuildFormatSelector(videoID, audioID string) string {
	if videoID != "" && audioID != "" {
		return videoID + "+" + audioID
	}
	if videoID != "" {
		return videoID
	}
	return audioID
}

// ---------------------------------------------------------------------------
// queue model
// ---------------------------------------------------------------------------

// Status is a job state; the labels are the user's own vocabulary.
type Status string

const (
	Loaded      Status = "cargada"
	Downloading Status = "descargando"
	Done        Status = "completado"
	Error       Status = "error"
	Cancelled   Status = "cancelado"
)

// StatusLabel renders the estado cell.
func StatusLabel(status Status, percent float64, errText string) string {
	switch status {
	case Downloading:
		return fmt.Sprintf("descargando (%.0f%%)", percent)
	case Error:
		if errText != "" {
			return "error: " + errText
		}
		return "error"
	}
	return string(status)
}

// DefaultSelection picks the best video and, when that stream carries no
// audio, the best matching audio.
func DefaultSelection(fmts []Fmt, preferAudioCodec string) (string, string) {
	var videos, progressiveVideos, muxed, progressiveMuxed []Fmt
	for _, f := range fmts {
		if !f.Available {
			continue
		}
		switch f.Kind {
		case Video:
			videos = append(videos, f)
			if !f.IsHLS() {
				progressiveVideos = append(progressiveVideos, f)
			}
		case Muxed:
			muxed = append(muxed, f)
			if !f.IsHLS() {
				progressiveMuxed = append(progressiveMuxed, f)
			}
		}
	}
	candidates := videos
	if len(progressiveVideos) > 0 {
		candidates = progressiveVideos
	}
	best := bestByQuality(candidates)
	if best == nil {
		candidates = muxed
		if len(progressiveMuxed) > 0 {
			candidates = progressiveMuxed
		}
		best = bestByQuality(candidates)
	}
	if best == nil {
		return "", ""
	}
	if best.Kind == Muxed {
		return best.ID, ""
	}

	audios := make([]Fmt, 0, len(fmts))
	for _, f := range fmts {
		if f.Kind == Audio {
			audios = append(audios, f)
		}
	}
	if len(audios) == 0 {
		return best.ID, ""
	}
	if preferAudioCodec != "" {
		preferred := make([]Fmt, 0, len(audios))
		for _, a := range audios {
			if a.ACodecName() == preferAudioCodec {
				preferred = append(preferred, a)
			}
		}
		if len(preferred) > 0 {
			audios = preferred
		}
	}
	chosen := audios[0]
	for _, a := range audios[1:] {
		if betterAudio(a, chosen) {
			chosen = a
		}
	}
	return best.ID, chosen.ID
}

func videoQuality(f Fmt) [3]float64 {
	return [3]float64{float64(f.Height), f.FPS, f.TBR}
}

func audioQuality(f Fmt) [2]float64 {
	var size float64
	if f.Size != nil {
		size = float64(*f.Size)
	}
	rate := f.ABR
	if rate == 0 {
		rate = f.TBR
	}
	return [2]float64{rate, size}
}

func betterAudio(left, right Fmt) bool {
	a, b := audioQuality(left), audioQuality(right)
	if a[0] != b[0] {
		return a[0] > b[0]
	}
	return a[1] > b[1]
}

func bestByQuality(candidates []Fmt) *Fmt {
	if len(candidates) == 0 {
		return nil
	}
	best := candidates[0]
	for _, f := range candidates[1:] {
		left, right := videoQuality(f), videoQuality(best)
		if left[0] > right[0] ||
			(left[0] == right[0] && (left[1] > right[1] ||
				(left[1] == right[1] && left[2] > right[2]))) {
			best = f
		}
	}
	return &best
}

// Job is one queue row: a URL plus its formats, choice and download state.
type Job struct {
	UID      int
	URL      string
	Title    string
	Uploader string
	Duration float64
	Formats  []Fmt
	VideoID  string
	AudioID  string
	Status   Status
	Percent  float64
	Speed    string
	ETA      string
	Size     string
	Error    string
	Path     string
}

// NewJob builds a row in its initial state.
func NewJob(uid int, url string) *Job {
	return &Job{UID: uid, URL: url, Status: Loaded}
}

// Analyzed reports whether the row already has formats.
func (j *Job) Analyzed() bool { return len(j.Formats) > 0 }

// Find returns the format with that id, or nil.
func (j *Job) Find(id string) *Fmt {
	if id == "" {
		return nil
	}
	for i := range j.Formats {
		if j.Formats[i].ID == id {
			return &j.Formats[i]
		}
	}
	return nil
}

// Video is the chosen video stream, if any.
func (j *Job) Video() *Fmt { return j.Find(j.VideoID) }

// Audio is the chosen audio stream, if any.
func (j *Job) Audio() *Fmt { return j.Find(j.AudioID) }

// Selector is the explicit `-f` value; empty means "let the config decide".
func (j *Job) Selector() string { return BuildFormatSelector(j.VideoID, j.AudioID) }

// Label is the title column, truncated to limit runes.
func (j *Job) Label(limit int) string {
	text := j.Title
	if text == "" {
		text = j.URL
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit-1]) + "…"
}

// QualityLabel is the calidad column.
func (j *Job) QualityLabel() string {
	if !j.Analyzed() || j.VideoID == "" {
		return "auto (best)"
	}
	video := j.Video()
	if video == nil {
		return "auto (best)"
	}
	codec := video.VCodecName()
	if codec == "" {
		codec = "?"
	}
	if video.Kind == Muxed {
		return fmt.Sprintf("%s · %s (muxed)", video.Resolution(), codec)
	}
	return fmt.Sprintf("%s · %s", video.Resolution(), codec)
}

// AudioLabel is the audio column.
func (j *Job) AudioLabel() string {
	if video := j.Video(); j.Analyzed() && video != nil && video.Kind == Muxed {
		return "— (incluido)"
	}
	if !j.Analyzed() || j.AudioID == "" {
		return "auto (best)"
	}
	audio := j.Audio()
	if audio == nil {
		return "auto (best)"
	}
	codec := audio.ACodecName()
	if codec == "" {
		codec = "?"
	}
	rate := audio.ABR
	if rate == 0 {
		rate = audio.TBR
	}
	return fmt.Sprintf("%s %s", codec, HumanBitrate(rate))
}

// ExpectedSize sums the chosen streams, falling back to the video stream.
func (j *Job) ExpectedSize() *int64 {
	var total int64
	counted := false
	for _, f := range []*Fmt{j.Video(), j.Audio()} {
		if f != nil && f.Size != nil {
			total += *f.Size
			counted = true
		}
	}
	if counted {
		return &total
	}
	if video := j.Video(); video != nil && video.Size != nil {
		value := *video.Size
		return &value
	}
	return nil
}

// SizeLabel is the tamaño column.
func (j *Job) SizeLabel() string { return HumanSize(j.ExpectedSize()) }
