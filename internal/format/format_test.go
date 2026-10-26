package format

import (
	"strings"
	"testing"
)

// fixtureJSON mirrors real `yt-dlp -J` output (Big Buck Bunny 4K probe).
const fixtureJSON = `{
  "_type": "video",
  "id": "aqz-KE-bpKQ",
  "title": "Big Buck Bunny 60fps 4K",
  "uploader": "Blender",
  "duration": 635,
  "is_live": false,
  "webpage_url": "https://www.youtube.com/watch?v=aqz-KE-bpKQ",
  "formats": [
    {"format_id": "sb0", "ext": "mhtml", "vcodec": "none", "acodec": "none",
     "height": 180, "fps": 0.2, "protocol": "mhtml", "format_note": "storyboard"},
    {"format_id": "140", "ext": "m4a", "vcodec": "none", "acodec": "mp4a.40.2",
     "abr": 129.5, "tbr": 129.5, "filesize": 10271496, "protocol": "https",
     "format_note": "medium"},
    {"format_id": "251", "ext": "webm", "vcodec": "none", "acodec": "opus",
     "abr": 128.6, "tbr": 128.6, "filesize": 10202210, "protocol": "https",
     "format_note": "medium"},
    {"format_id": "18", "ext": "mp4", "vcodec": "avc1.42001E", "acodec": "mp4a.40.2",
     "height": 360, "width": 640, "fps": 30, "tbr": 359.6,
     "filesize": 28526904, "protocol": "https", "format_note": "360p",
     "dynamic_range": "SDR"},
    {"format_id": "299", "ext": "mp4", "vcodec": "avc1.64002A", "acodec": "none",
     "height": 1080, "width": 1920, "fps": 60, "tbr": 3247.8,
     "filesize": 257619653, "protocol": "https", "format_note": "1080p60",
     "dynamic_range": "SDR"},
    {"format_id": "303", "ext": "webm", "vcodec": "vp9", "acodec": "none",
     "height": 1080, "width": 1920, "fps": 60, "tbr": 2127.2,
     "filesize": 168736189, "protocol": "https", "format_note": "1080p60",
     "dynamic_range": "SDR"},
    {"format_id": "401", "ext": "mp4", "vcodec": "av01.0.13M.0", "acodec": "none",
     "height": 2160, "width": 3840, "fps": 60, "tbr": 8981.8,
     "filesize_approx": 712445280, "protocol": "https", "format_note": "2160p60",
     "dynamic_range": "HDR10"},
    {"format_id": "301", "ext": "mp4", "vcodec": "avc1.64002A", "acodec": "mp4a.40.2",
     "height": 1080, "fps": 60, "tbr": 7417.7, "protocol": "m3u8_native",
     "dynamic_range": "SDR"}
  ]
}`

func fixtureFormats(t *testing.T) []Fmt {
	t.Helper()
	_, formats, err := UnmarshalInfo([]byte(fixtureJSON))
	if err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	return formats
}

func fixtureByID(t *testing.T) map[string]Fmt {
	t.Helper()
	out := map[string]Fmt{}
	for _, f := range fixtureFormats(t) {
		out[f.ID] = f
	}
	return out
}

func TestCodecNameMapping(t *testing.T) {
	cases := map[string]string{
		"avc1.64002A":   "h264",
		"av01.0.13M.0":  "av1",
		"vp9":           "vp9",
		"vp09.00.10.08": "vp9",
		"hev1.1.6.L93":  "h265",
		"hvc1.2.4":      "h265",
		"av01":          "av1",
		"mp4a.40.2":     "aac",
		"opus":          "opus",
		"ec-3":          "eac3",
		"ac-3":          "ac3",
		"none":          "",
		"":              "",
	}
	for raw, want := range cases {
		if got := CodecName(raw); got != want {
			t.Errorf("CodecName(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestParseFormatsClassifiesKinds(t *testing.T) {
	rows := fixtureByID(t)
	if rows["sb0"].Kind != Image || rows["sb0"].Available {
		t.Errorf("storyboard: kind=%q available=%v, want image/false", rows["sb0"].Kind, rows["sb0"].Available)
	}
	if rows["140"].Kind != Audio || !rows["140"].Available {
		t.Errorf("140: kind=%q available=%v, want audio/true", rows["140"].Kind, rows["140"].Available)
	}
	if rows["299"].Kind != Video {
		t.Errorf("299 kind = %q, want video", rows["299"].Kind)
	}
	if rows["18"].Kind != Muxed {
		t.Errorf("18 kind = %q, want muxed", rows["18"].Kind)
	}
	if !rows["301"].IsHLS() {
		t.Error("301 should be HLS")
	}
	if rows["299"].IsHLS() {
		t.Error("299 should not be HLS")
	}
}

func TestParseFormatsResolutionAndLabels(t *testing.T) {
	rows := fixtureByID(t)
	cases := []struct{ id, want string }{
		{"299", "1080p60"},
		{"18", "360p"},
		{"140", "audio"},
		{"sb0", "180p"},
	}
	for _, c := range cases {
		if got := rows[c.id].Resolution(); got != c.want {
			t.Errorf("%s resolution = %q, want %q", c.id, got, c.want)
		}
	}
	if got := rows["140"].VCodecName(); got != "" {
		t.Errorf("140 video codec = %q, want empty", got)
	}
	if got := rows["140"].ACodecName(); got != "aac" {
		t.Errorf("140 audio codec = %q, want aac", got)
	}
	if got := rows["401"].HDR; got != "HDR10" {
		t.Errorf("401 hdr = %q, want HDR10", got)
	}
	if got := rows["401"].HDRLabel(); got != "HDR10" {
		t.Errorf("401 hdr label = %q, want HDR10", got)
	}
	if got := rows["299"].HDRLabel(); got != "" {
		t.Errorf("SDR should have no label, got %q", got)
	}
}

func TestSizeUsesApproxWhenMissing(t *testing.T) {
	rows := fixtureByID(t)
	if rows["140"].Size == nil || *rows["140"].Size != 10271496 {
		t.Errorf("140 size = %v, want 10271496", rows["140"].Size)
	}
	if rows["401"].Size == nil || *rows["401"].Size != 712445280 {
		t.Errorf("401 size = %v, want 712445280 (filesize_approx)", rows["401"].Size)
	}
}

func TestHumanSize(t *testing.T) {
	n := func(v int64) *int64 { return &v }
	cases := []struct {
		in   *int64
		want string
	}{
		{nil, "—"},
		{n(0), "0 B"},
		{n(1023), "1023 B"},
		{n(1024), "1.0 KiB"},
		{n(150524867), "143.6 MiB"},
	}
	for _, c := range cases {
		if got := HumanSize(c.in); got != c.want {
			t.Errorf("HumanSize(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHumanDurationAndBitrate(t *testing.T) {
	if got := HumanDuration(0); got != "—" {
		t.Errorf("HumanDuration(0) = %q", got)
	}
	if got := HumanDuration(635); got != "10:35" {
		t.Errorf("HumanDuration(635) = %q, want 10:35", got)
	}
	if got := HumanDuration(3725); got != "1:02:05" {
		t.Errorf("HumanDuration(3725) = %q, want 1:02:05", got)
	}
	if got := HumanBitrate(0); got != "—" {
		t.Errorf("HumanBitrate(0) = %q", got)
	}
	if got := HumanBitrate(129.5); got != "130k" {
		t.Errorf("HumanBitrate(129.5) = %q, want 130k", got)
	}
	if got := HumanBitrate(3247.8); got != "3.2M" {
		t.Errorf("HumanBitrate(3247.8) = %q, want 3.2M", got)
	}
}

func TestApplyFiltersByKind(t *testing.T) {
	formats := fixtureFormats(t)
	for _, item := range ApplyFilters(formats, Filters{Kind: "video"}) {
		if item.Kind != Video {
			t.Errorf("kind filter returned %q", item.Kind)
		}
	}
	if ids := idsOf(ApplyFilters(formats, Filters{Kind: "audio"})); len(ids) != 2 {
		t.Errorf("audio filter returned %v", ids)
	}
	if !contains(idsOf(ApplyFilters(formats, Filters{Kind: "muxed"})), "18") {
		t.Error("muxed filter should include 18")
	}
}

func TestApplyFiltersHidesImagesByDefault(t *testing.T) {
	formats := fixtureFormats(t)
	for _, item := range ApplyFilters(formats, Filters{}) {
		if item.Kind == Image {
			t.Fatalf("storyboard leaked into the default view: %q", item.ID)
		}
	}
	if !contains(idsOf(ApplyFilters(formats, Filters{ShowImages: true})), "sb0") {
		t.Error("ShowImages should expose sb0")
	}
}

func TestApplyFiltersByFreeText(t *testing.T) {
	formats := fixtureFormats(t)
	if got := idsOf(ApplyFilters(formats, Filters{Text: "1080"})); strings.Join(got, ",") != "299,303,301" {
		t.Errorf("text 1080 = %v, want order 299,303,301", got)
	}
	if got := idsOf(ApplyFilters(formats, Filters{Text: "av1"})); strings.Join(got, ",") != "401" {
		t.Errorf("text av1 = %v", got)
	}
	if got := idsOf(ApplyFilters(formats, Filters{Text: "opus"})); strings.Join(got, ",") != "251" {
		t.Errorf("text opus = %v", got)
	}
	if got := ApplyFilters(formats, Filters{Text: "4080"}); len(got) != 0 {
		t.Errorf("text 4080 = %v, want none", idsOf(got))
	}
}

func TestApplyFiltersByContainerAndSize(t *testing.T) {
	formats := fixtureFormats(t)
	if got := idsOf(ApplyFilters(formats, Filters{Container: "m4a"})); strings.Join(got, ",") != "140" {
		t.Errorf("container m4a = %v", got)
	}
	sized := ApplyFilters(formats, Filters{OnlyWithSize: true})
	for _, item := range sized {
		if item.Size == nil {
			t.Errorf("%s has no size but survived OnlyWithSize", item.ID)
		}
	}
	if contains(idsOf(sized), "301") {
		t.Error("301 has no size and should be filtered")
	}
}

func TestApplyFiltersHidesHLS(t *testing.T) {
	formats := fixtureFormats(t)
	if contains(idsOf(ApplyFilters(formats, Filters{HideHLS: true})), "301") {
		t.Error("301 is HLS and should be hidden")
	}
	if !contains(idsOf(ApplyFilters(formats, Filters{})), "301") {
		t.Error("HLS should be visible by default")
	}
}

func TestAvailabilityReport(t *testing.T) {
	report := AvailabilityReport(fixtureFormats(t), "mp4")
	var text strings.Builder
	for _, line := range report {
		text.WriteString(strings.ToLower(line.Text) + "\n")
	}
	joined := text.String()
	for _, needle := range []string{"2160p", "video-only", "av1", "opus", "aac", "mp4"} {
		if !strings.Contains(joined, needle) {
			t.Errorf("report missing %q:\n%s", needle, joined)
		}
	}
	if len(report) == 0 || report[0].Kind != LineResolution {
		t.Errorf("first line should be a resolution line, got %+v", report[0])
	}
}

func TestFormatSelector(t *testing.T) {
	cases := []struct{ video, audio, want string }{
		{"299", "140", "299+140"},
		{"18", "", "18"},
		{"", "140", "140"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := BuildFormatSelector(c.video, c.audio); got != c.want {
			t.Errorf("BuildFormatSelector(%q,%q) = %q, want %q", c.video, c.audio, got, c.want)
		}
	}
}

func TestStatusLabelVariants(t *testing.T) {
	if got := StatusLabel(Loaded, 0, ""); got != "cargada" {
		t.Errorf("loaded = %q", got)
	}
	if got := StatusLabel(Downloading, 45.4, ""); got != "descargando (45%)" {
		t.Errorf("downloading = %q", got)
	}
	if got := StatusLabel(Done, 100, ""); got != "completado" {
		t.Errorf("done = %q", got)
	}
	if got := StatusLabel(Error, 0, "boom"); got != "error: boom" {
		t.Errorf("error = %q", got)
	}
	if got := StatusLabel(Error, 0, ""); got != "error" {
		t.Errorf("error without text = %q", got)
	}
}

func TestDefaultSelection(t *testing.T) {
	videoID, audioID := DefaultSelection(fixtureFormats(t), "")
	if videoID != "401" {
		t.Errorf("video = %q, want 401 (2160p av1 video-only)", videoID)
	}
	if audioID != "140" {
		t.Errorf("audio = %q, want 140 (highest bitrate)", audioID)
	}
}

func TestDefaultSelectionMuxedOnly(t *testing.T) {
	only18 := []Fmt{}
	for _, f := range fixtureFormats(t) {
		if f.ID == "18" {
			only18 = append(only18, f)
		}
	}
	videoID, audioID := DefaultSelection(only18, "")
	if videoID != "18" || audioID != "" {
		t.Errorf("muxed-only selection = (%q,%q), want (18,\"\")", videoID, audioID)
	}
}

func TestDefaultSelectionPrefersProgressiveWhenPresent(t *testing.T) {
	formats := fixtureFormats(t)
	// Remove every non-HLS video: only the HLS muxed 301 and the HLS-less set
	// remains, so the HLS variant is what the fallback can use.
	var hlsOnly []Fmt
	for _, f := range formats {
		if f.IsHLS() || f.Kind == Audio || f.Kind == Image {
			hlsOnly = append(hlsOnly, f)
		}
	}
	videoID, _ := DefaultSelection(hlsOnly, "")
	if videoID != "301" {
		t.Errorf("HLS-only selection = %q, want 301", videoID)
	}
}

func TestJobLabelsAndSelector(t *testing.T) {
	job := NewJob(1, "https://youtu.be/x")
	if job.Selector() != "" {
		t.Errorf("fresh selector = %q", job.Selector())
	}
	if got := job.QualityLabel(); got != "auto (best)" {
		t.Errorf("fresh quality = %q", got)
	}
	if got := job.AudioLabel(); got != "auto (best)" {
		t.Errorf("fresh audio = %q", got)
	}

	job.Formats = fixtureFormats(t)
	job.VideoID, job.AudioID = "299", "140"
	if got := job.Selector(); got != "299+140" {
		t.Errorf("selector = %q", got)
	}
	if got := job.QualityLabel(); got != "1080p60 · h264" {
		t.Errorf("quality = %q", got)
	}
	if got := job.AudioLabel(); got != "aac 130k" {
		t.Errorf("audio = %q", got)
	}
	if got := job.SizeLabel(); got == "—" {
		t.Error("size label should be known for 299+140")
	}

	job.VideoID, job.AudioID = "18", ""
	if got := job.Selector(); got != "18" {
		t.Errorf("muxed selector = %q", got)
	}
	if got := job.AudioLabel(); got != "— (incluido)" {
		t.Errorf("muxed audio = %q", got)
	}
	if got := job.QualityLabel(); got != "360p · h264 (muxed)" {
		t.Errorf("muxed quality = %q", got)
	}
}

func TestJobLabelTruncatesByRunes(t *testing.T) {
	job := NewJob(1, "https://example.com")
	job.Title = strings.Repeat("ñ", 80)
	got := job.Label(58)
	if len([]rune(got)) != 58 {
		t.Errorf("label runes = %d, want 58", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("label should end with an ellipsis: %q", got)
	}
	if short := job.Label(200); short != job.Title {
		t.Errorf("long limit should keep the title")
	}
}

func TestIsCompatibleMatrix(t *testing.T) {
	video := func(codec string) Fmt {
		return Fmt{ID: "v", Ext: "mp4", VCodec: codec, Kind: Video, Available: true}
	}
	audio := func(codec string) Fmt {
		return Fmt{ID: "a", Ext: "m4a", ACodec: codec, Kind: Audio, Available: true}
	}
	cases := []struct {
		name  string
		f     Fmt
		merge string
		want  bool
	}{
		{"h264 in mp4", video("avc1.64002A"), "mp4", true},
		{"vp9 in mp4", video("vp09.00.10.08"), "mp4", false},
		{"av1 in mp4", video("av01.0.13M.0"), "mp4", true},
		{"aac in mp4", audio("mp4a.40.2"), "mp4", true},
		{"opus in mp4", audio("opus"), "mp4", false},
		{"opus in webm", audio("opus"), "webm", true},
		{"vp9 in mkv", video("vp09.00.10.08"), "mkv", true},
		{"muxed always fine", Fmt{ID: "m", VCodec: "vp9", ACodec: "opus", Kind: Muxed}, "mp4", true},
		{"no merge format", video("vp09.00.10.08"), "", true},
		{"mov family", video("vp9"), "mov", false},
	}
	for _, c := range cases {
		if got := IsCompatible(c.f, c.merge); got != c.want {
			t.Errorf("%s: IsCompatible = %v, want %v", c.name, got, c.want)
		}
	}
	if got := CompatibilityNote(audio("opus"), "mp4"); got != "opus not compatible with mp4" {
		t.Errorf("note = %q", got)
	}
	if got := CompatibilityNote(audio("mp4a.40.2"), "mp4"); got != "" {
		t.Errorf("compatible note = %q, want empty", got)
	}
	if got := ContainerFamily(".matroska"); got != "mkv" {
		t.Errorf("ContainerFamily(.matroska) = %q", got)
	}
}

func TestParseFormatsSkipsRowsWithoutID(t *testing.T) {
	payload := `{"formats":[{"ext":"mp4","vcodec":"avc1","acodec":"mp4a"},{"format_id":"140","ext":"m4a","acodec":"mp4a"}]}`
	_, formats, err := UnmarshalInfo([]byte(payload))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(formats) != 1 || formats[0].ID != "140" {
		t.Errorf("formats = %+v, want only 140", formats)
	}
}

func TestParseFormatsCoercesNumericID(t *testing.T) {
	payload := `{"formats":[{"format_id":18,"ext":"mp4","vcodec":"avc1","acodec":"mp4a"}]}`
	_, formats, err := UnmarshalInfo([]byte(payload))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(formats) != 1 || formats[0].ID != "18" {
		t.Errorf("formats = %+v, want id \"18\"", formats)
	}
}

func TestUnknownContainerFallsBackToMp4Rules(t *testing.T) {
	vp9 := Fmt{ID: "v", VCodec: "vp9", Kind: Video}
	if IsCompatible(vp9, "avi") {
		t.Error("unknown container should judge video codecs with the mp4 rules")
	}
}

func idsOf(formats []Fmt) []string {
	out := make([]string, 0, len(formats))
	for _, f := range formats {
		out = append(out, f.ID)
	}
	return out
}

func contains(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}
