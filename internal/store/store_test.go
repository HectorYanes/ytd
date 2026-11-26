package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytd/internal/format"
)

func sizeOf(value int64) *int64 { return &value }

func TestStateDirHonorsXDGAndEveryPlatform(t *testing.T) {
	if got, want := StateDir("linux", "/home/u", map[string]string{"XDG_STATE_HOME": "/state"}), filepath.Join("/state", "ytd"); got != want {
		t.Errorf("linux xdg = %q, want %q", got, want)
	}
	if got, want := StateDir("linux", "/home/u", map[string]string{}), filepath.Join("/home/u", ".local", "state", "ytd"); got != want {
		t.Errorf("linux fallback = %q, want %q", got, want)
	}
	if got, want := StateDir("darwin", "/Users/u", map[string]string{}), filepath.Join("/Users/u", "Library", "Application Support", "ytd"); got != want {
		t.Errorf("darwin = %q, want %q", got, want)
	}
	if got, want := StateDir("windows", `/home/u`, map[string]string{"LOCALAPPDATA": `/appdata`}), filepath.Join(`/appdata`, "ytd"); got != want {
		t.Errorf("windows = %q, want %q", got, want)
	}
	if got, want := StateDir("Windows", `/home/u`, map[string]string{}), filepath.Join(`/home/u`, "AppData", "Local", "ytd"); got != want {
		t.Errorf("windows fallback = %q, want %q", got, want)
	}
}

func TestLoadMissingFileIsAnEmptyQueue(t *testing.T) {
	restored := New(filepath.Join(t.TempDir(), "nope", "queue.json")).Load()
	if len(restored.Jobs) != 0 || len(restored.Resume) != 0 || restored.Warning != "" {
		t.Errorf("restored = %+v", restored)
	}
}

func TestLoadSkipsACorruptFileWithAWarning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	restored := New(path).Load()
	if len(restored.Jobs) != 0 {
		t.Errorf("a corrupt file produced %d rows", len(restored.Jobs))
	}
	if !strings.Contains(restored.Warning, "unreadable") {
		t.Errorf("warning = %q", restored.Warning)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ytd", "queue.json")
	jobs := []format.Job{{
		UID:      3,
		URL:      "https://example.test/a",
		Title:    "A video",
		Uploader: "someone",
		Duration: 61.5,
		Formats: []format.Fmt{
			{ID: "137", Ext: "mp4", Kind: format.Video, VCodec: "avc1.640028", Height: 1080, Width: 1920, FPS: 30, TBR: 4400, Size: sizeOf(120), Note: "1080p", HDR: "HDR10", Language: "es", Protocol: "https", Available: true},
			{ID: "140", Ext: "m4a", Kind: format.Audio, ACodec: "mp4a.40.2", ABR: 128, TBR: 128, Size: sizeOf(4), Available: true},
		},
		VideoID: "137",
		AudioID: "140",
		Status:  format.Loaded,
		Percent: 0,
		Size:    "1.2 GiB",
	}}
	if err := New(path).Save(jobs); err != nil {
		t.Fatalf("save: %v", err)
	}

	restored := New(path).Load()
	if restored.Warning != "" {
		t.Fatalf("warning = %q", restored.Warning)
	}
	if len(restored.Jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(restored.Jobs))
	}
	got := restored.Jobs[0]
	if got.UID != 3 || got.Title != "A video" || got.VideoID != "137" || got.AudioID != "140" || got.Status != format.Loaded {
		t.Errorf("row = %+v", got)
	}
	if len(got.Formats) != 2 {
		t.Fatalf("formats = %d, want 2", len(got.Formats))
	}
	video := got.Formats[0]
	if video.ID != "137" || video.Kind != format.Video || video.Height != 1080 || video.HDR != "HDR10" || !video.Available {
		t.Errorf("video = %+v", video)
	}
	if video.Size == nil || *video.Size != 120 {
		t.Errorf("size = %v", video.Size)
	}
}

func TestLoadResumesAnInterruptedDownload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	writeQueue(t, path, map[string]any{
		"version": 1,
		"jobs": []any{
			map[string]any{"uid": 1, "url": "https://example.test/a", "status": string(format.Loaded)},
			map[string]any{"uid": 2, "url": "https://example.test/b", "status": string(format.Downloading), "percent": 42.0},
		},
	})

	restored := New(path).Load()
	if len(restored.Jobs) != 2 {
		t.Fatalf("jobs = %d, want 2", len(restored.Jobs))
	}
	if len(restored.Resume) != 1 || restored.Resume[0] != 2 {
		t.Errorf("resume = %v, want [2]", restored.Resume)
	}
	if restored.Jobs[1].Status != format.Loaded {
		t.Errorf("status = %q, want %q", restored.Jobs[1].Status, format.Loaded)
	}
}

func TestLoadDropsDuplicateURLsAndFixesUIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	writeQueue(t, path, map[string]any{
		"version": 1,
		"jobs": []any{
			map[string]any{"uid": 0, "url": "https://example.test/a"},
			map[string]any{"uid": 0, "url": "https://example.test/a"},
			map[string]any{"uid": 0, "url": "https://example.test/b"},
		},
	})

	restored := New(path).Load()
	if len(restored.Jobs) != 2 {
		t.Fatalf("jobs = %d, want 2", len(restored.Jobs))
	}
	if restored.Jobs[0].UID != 1 || restored.Jobs[1].UID != 2 {
		t.Errorf("uids = %d/%d, want 1/2", restored.Jobs[0].UID, restored.Jobs[1].UID)
	}
}

func TestLoadRejectsAFileThatIsNotAnObject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	if err := os.WriteFile(path, []byte(`[1,2,3]`), 0o600); err != nil {
		t.Fatal(err)
	}
	restored := New(path).Load()
	if len(restored.Jobs) != 0 || restored.Warning == "" {
		t.Errorf("restored = %+v", restored)
	}
}

func TestSaveCreatesTheDirectoryAndLeavesNoTemporaryFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "ytd")
	path := filepath.Join(dir, "queue.json")
	store := New(path)
	for index := 0; index < 2; index++ {
		if err := store.Save([]format.Job{{UID: 1, URL: "https://example.test/a", Status: format.Loaded}}); err != nil {
			t.Fatalf("save %d: %v", index, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".queue-") {
			t.Errorf("temporary file left behind: %s", entry.Name())
		}
	}
}

func writeQueue(t *testing.T, path string, payload map[string]any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
