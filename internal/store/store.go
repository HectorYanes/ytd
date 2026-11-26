// Package store persists the queue between runs: the TUI dies, the queue does
// not. State lives outside the project and outside the download folder, one
// JSON file per user. Reads are defensive on purpose: a corrupt or foreign file
// must never block the app from starting, it just starts with an empty queue.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"ytd/internal/format"
)

// Version is the schema of the queue file.
const Version = 1

// Restored is what came back from disk, plus which rows must be re-queued.
type Restored struct {
	Jobs    []format.Job
	Resume  []int
	Warning string
}

// Store reads and writes one queue file.
type Store struct {
	Path string
}

// New returns a store bound to a path.
func New(path string) *Store { return &Store{Path: path} }

// QueuePath is the per-user queue file.
func QueuePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(StateDir(runtime.GOOS, home, environ()), "queue.json"), nil
}

// StateDir is the per-user state directory, honoring XDG on every POSIX system
// and LOCALAPPDATA on Windows. The system name is matched case-insensitively so
// both runtime.GOOS and platform.system() spellings work.
func StateDir(system, home string, env map[string]string) string {
	switch strings.ToLower(system) {
	case "windows":
		base := env["LOCALAPPDATA"]
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "ytd")
	}
	if xdg := env["XDG_STATE_HOME"]; xdg != "" {
		return filepath.Join(xdg, "ytd")
	}
	if strings.ToLower(system) == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "ytd")
	}
	return filepath.Join(home, ".local", "state", "ytd")
}

func environ() map[string]string {
	entries := os.Environ()
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// (de)serialization
// ---------------------------------------------------------------------------

type queueFile struct {
	Version int      `json:"version"`
	Jobs    []jobDTO `json:"jobs"`
}

type jobDTO struct {
	UID      int      `json:"uid"`
	URL      string   `json:"url"`
	Title    string   `json:"title"`
	Uploader string   `json:"uploader"`
	Duration float64  `json:"duration"`
	Formats  []fmtDTO `json:"formats"`
	VideoID  string   `json:"video_id"`
	AudioID  string   `json:"audio_id"`
	Status   string   `json:"status"`
	Percent  float64  `json:"percent"`
	Speed    string   `json:"speed"`
	ETA      string   `json:"eta"`
	Size     string   `json:"size"`
	Error    string   `json:"error"`
	Path     string   `json:"path"`
}

type fmtDTO struct {
	ID        string  `json:"id"`
	Ext       string  `json:"ext"`
	VCodec    string  `json:"vcodec"`
	ACodec    string  `json:"acodec"`
	Height    int     `json:"height"`
	Width     int     `json:"width"`
	FPS       float64 `json:"fps"`
	TBR       float64 `json:"tbr"`
	ABR       float64 `json:"abr"`
	Size      *int64  `json:"size"`
	Note      string  `json:"note"`
	HDR       string  `json:"hdr"`
	Language  string  `json:"language"`
	Protocol  string  `json:"protocol"`
	Kind      string  `json:"kind"`
	Available *bool   `json:"available"`
}

func jobsToDTO(jobs []format.Job) []jobDTO {
	out := make([]jobDTO, 0, len(jobs))
	for _, job := range jobs {
		formats := make([]fmtDTO, 0, len(job.Formats))
		for _, candidate := range job.Formats {
			available := candidate.Available
			formats = append(formats, fmtDTO{
				ID: candidate.ID, Ext: candidate.Ext,
				VCodec: candidate.VCodec, ACodec: candidate.ACodec,
				Height: candidate.Height, Width: candidate.Width,
				FPS: candidate.FPS, TBR: candidate.TBR, ABR: candidate.ABR,
				Size: candidate.Size, Note: candidate.Note, HDR: candidate.HDR,
				Language: candidate.Language, Protocol: candidate.Protocol,
				Kind: string(candidate.Kind), Available: &available,
			})
		}
		out = append(out, jobDTO{
			UID: job.UID, URL: job.URL, Title: job.Title, Uploader: job.Uploader,
			Duration: job.Duration, Formats: formats,
			VideoID: job.VideoID, AudioID: job.AudioID,
			Status: string(job.Status), Percent: job.Percent,
			Speed: job.Speed, ETA: job.ETA, Size: job.Size,
			Error: job.Error, Path: job.Path,
		})
	}
	return out
}

func statusFromDTO(value string) format.Status {
	switch status := format.Status(value); status {
	case format.Loaded, format.Downloading, format.Done, format.Error, format.Cancelled:
		return status
	}
	return format.Loaded
}

func kindFromDTO(dto fmtDTO) format.Kind {
	switch kind := format.Kind(dto.Kind); kind {
	case format.Muxed, format.Video, format.Audio, format.Image:
		return kind
	}
	hasVideo := dto.VCodec != "" && dto.VCodec != "none"
	hasAudio := dto.ACodec != "" && dto.ACodec != "none"
	switch {
	case dto.Ext == "mhtml" || dto.Ext == "mhtml_list" || (!hasVideo && !hasAudio):
		return format.Image
	case hasVideo && hasAudio:
		return format.Muxed
	case hasVideo:
		return format.Video
	}
	return format.Audio
}

func formatFromDTO(dto fmtDTO) (format.Fmt, bool) {
	if dto.ID == "" {
		return format.Fmt{}, false
	}
	kind := kindFromDTO(dto)
	available := kind != format.Image
	if dto.Available != nil {
		available = *dto.Available
	}
	return format.Fmt{
		ID: dto.ID, Ext: dto.Ext, VCodec: dto.VCodec, ACodec: dto.ACodec,
		Height: dto.Height, Width: dto.Width,
		FPS: dto.FPS, TBR: dto.TBR, ABR: dto.ABR, Size: dto.Size,
		Note: dto.Note, HDR: dto.HDR, Language: dto.Language,
		Protocol: dto.Protocol, Kind: kind, Available: available,
	}, true
}

func jobFromDTO(dto jobDTO) (format.Job, bool) {
	url := strings.TrimSpace(dto.URL)
	if url == "" {
		return format.Job{}, false
	}
	formats := make([]format.Fmt, 0, len(dto.Formats))
	for _, entry := range dto.Formats {
		if candidate, ok := formatFromDTO(entry); ok {
			formats = append(formats, candidate)
		}
	}
	return format.Job{
		UID: dto.UID, URL: url, Title: dto.Title, Uploader: dto.Uploader,
		Duration: dto.Duration, Formats: formats,
		VideoID: dto.VideoID, AudioID: dto.AudioID,
		Status: statusFromDTO(dto.Status), Percent: dto.Percent,
		Speed: dto.Speed, ETA: dto.ETA, Size: dto.Size,
		Error: dto.Error, Path: dto.Path,
	}, true
}

// ---------------------------------------------------------------------------
// load and save
// ---------------------------------------------------------------------------

// Load reads the queue file, dropping duplicates and repairing UIDs. A row that
// was downloading when the app died comes back queued, and its UID is listed in
// Resume so the caller can submit it again.
func (s *Store) Load() Restored {
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return Restored{}
	}
	if err != nil {
		return Restored{Warning: fmt.Sprintf("queue: cannot read %s: %v", s.Path, err)}
	}

	var file queueFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return Restored{Warning: fmt.Sprintf("queue: ignoring unreadable %s", filepath.Base(s.Path))}
	}

	var (
		jobs    []format.Job
		resume  []int
		seenURL = map[string]bool{}
		seenUID = map[int]bool{}
		nextUID = 1
	)
	for _, entry := range file.Jobs {
		job, ok := jobFromDTO(entry)
		if !ok || seenURL[job.URL] {
			continue
		}
		if job.UID <= 0 || seenUID[job.UID] {
			job.UID = nextUID
		}
		seenURL[job.URL] = true
		seenUID[job.UID] = true
		if job.UID >= nextUID {
			nextUID = job.UID + 1
		}
		if job.Status == format.Downloading {
			job.Status = format.Loaded
			job.Percent = 0
			resume = append(resume, job.UID)
		}
		jobs = append(jobs, job)
	}
	return Restored{Jobs: jobs, Resume: resume}
}

// Save writes the queue atomically: a temporary file in the same directory is
// renamed over the target, so a crash never leaves half a queue behind.
func (s *Store) Save(jobs []format.Job) error {
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(queueFile{Version: Version, Jobs: jobsToDTO(jobs)}, "", " ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	handle, err := os.CreateTemp(dir, ".queue-*.tmp")
	if err != nil {
		return err
	}
	name := handle.Name()
	if _, err := handle.Write(payload); err != nil {
		handle.Close()
		os.Remove(name)
		return err
	}
	if err := handle.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, s.Path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
