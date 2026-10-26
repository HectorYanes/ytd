package ytdlp

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"ytd/internal/format"
)

// Proc is the part of a running yt-dlp process the pool needs. Tests provide
// fakes so the pool can be checked without spawning anything.
type Proc interface {
	Read(p []byte) (int, error)
	Wait() error
	Kill() error
}

// SpawnFunc starts one process. The context cancels it.
type SpawnFunc func(ctx context.Context, cmd, env []string) (Proc, error)

type execProc struct {
	cmd    *exec.Cmd
	output *os.File
}

func (p *execProc) Read(buffer []byte) (int, error) { return p.output.Read(buffer) }

func (p *execProc) Wait() error {
	err := p.cmd.Wait()
	p.output.Close()
	return err
}

func (p *execProc) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

// DefaultSpawn runs a real process with stdout and stderr merged, so the
// progress reader sees ERROR lines and progress lines in order.
func DefaultSpawn(ctx context.Context, cmd, env []string) (Proc, error) {
	if len(cmd) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	process := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	process.Env = env
	process.Stdout = writer
	process.Stderr = writer
	if err := process.Start(); err != nil {
		writer.Close()
		reader.Close()
		return nil, err
	}
	writer.Close()
	return &execProc{cmd: process, output: reader}, nil
}

// Manager is a bounded pool of yt-dlp downloads. It owns the job state and
// publishes snapshots, so callers never share mutable rows with it.
type Manager struct {
	OutDir        string
	MaxConcurrent int
	OnUpdate      func(format.Job)
	Spawn         SpawnFunc
	Prefix        []string

	mu          sync.Mutex
	jobs        map[int]*format.Job
	running     map[int]context.CancelFunc
	queued      map[int]bool
	sem         chan struct{}
	wg          sync.WaitGroup
	maxObserved int
}

// NewManager builds a pool. maxConcurrent below one is raised to one.
func NewManager(outDir string, maxConcurrent int, onUpdate func(format.Job)) *Manager {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Manager{
		OutDir:        outDir,
		MaxConcurrent: maxConcurrent,
		OnUpdate:      onUpdate,
		jobs:          map[int]*format.Job{},
		running:       map[int]context.CancelFunc{},
		queued:        map[int]bool{},
		sem:           make(chan struct{}, maxConcurrent),
	}
}

// Submit queues the given rows, skipping any uid already queued or running.
// It returns how many were actually added.
func (m *Manager) Submit(jobs ...format.Job) int {
	added := 0
	for _, job := range jobs {
		copyOfJob := job
		m.mu.Lock()
		if m.queued[job.UID] {
			m.mu.Unlock()
			continue
		}
		if _, running := m.running[job.UID]; running {
			m.mu.Unlock()
			continue
		}
		m.queued[job.UID] = true
		stored := &copyOfJob
		m.jobs[job.UID] = stored
		m.mu.Unlock()

		added++
		m.wg.Add(1)
		go m.run(job.UID)
	}
	return added
}

func (m *Manager) run(uid int) {
	defer m.wg.Done()
	m.sem <- struct{}{}
	defer func() { <-m.sem }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m.mu.Lock()
	job := m.jobs[uid]
	m.queued[uid] = false
	m.running[uid] = cancel
	if len(m.running) > m.maxObserved {
		m.maxObserved = len(m.running)
	}
	m.mu.Unlock()

	job.Status = format.Downloading
	job.Percent = 0
	job.Error = ""
	job.Speed = ""
	job.ETA = ""
	job.Size = ""
	m.emit(uid)

	prefix := m.Prefix
	var err error
	if len(prefix) == 0 {
		resolution, resolveErr := Resolve()
		if resolveErr != nil {
			err = resolveErr
		} else {
			prefix = resolution.Prefix
		}
	}
	if err == nil {
		err = m.download(ctx, uid, job, prefix)
	}

	m.mu.Lock()
	delete(m.running, uid)
	status := job.Status
	m.mu.Unlock()

	if status == format.Cancelled {
		m.emit(uid)
		return
	}
	if err != nil {
		var lastError string
		if exit, ok := err.(*exitFailure); ok {
			lastError = exit.message
		}
		if lastError == "" {
			lastError = err.Error()
		}
		m.mu.Lock()
		job.Status = format.Error
		job.Error = truncate(lastError, 120)
		m.mu.Unlock()
	} else {
		m.mu.Lock()
		job.Status = format.Done
		job.Percent = 100
		m.mu.Unlock()
	}
	m.emit(uid)
}

func (m *Manager) download(ctx context.Context, uid int, job *format.Job, prefix []string) error {
	spawn := m.Spawn
	if spawn == nil {
		spawn = DefaultSpawn
	}
	cmd := DownloadCommand(prefix, *job, m.OutDir)
	process, err := spawn(ctx, cmd, ChildEnv(nil))
	if err != nil {
		return err
	}

	reader := bufio.NewReaderSize(process, 64*1024)
	lastError := ""
	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			trimmed := strings.TrimRight(line, "\r\n")
			if marker := strings.Index(trimmed, "Destination:"); marker >= 0 {
				job.Path = strings.TrimSpace(trimmed[marker+len("Destination:"):])
			}
			if strings.HasPrefix(trimmed, "ERROR:") {
				lastError = strings.TrimSpace(trimmed[len("ERROR:"):])
			}
			if progress := ParseProgress(trimmed); progress != nil {
				job.Percent = progress.Percent
				job.Speed = progress.Speed
				job.ETA = progress.ETA
				job.Size = progress.Size
				m.emit(uid)
			}
		}
		if readErr != nil {
			break
		}
	}
	waitErr := process.Wait()
	if waitErr != nil {
		if lastError != "" {
			return &exitFailure{message: lastError, code: exitCode(waitErr)}
		}
		return waitErr
	}
	return nil
}

type exitFailure struct {
	message string
	code    int
}

func (e *exitFailure) Error() string {
	if e.code > 0 {
		return fmt.Sprintf("exit %d: %s", e.code, e.message)
	}
	return e.message
}

func exitCode(err error) int {
	var exit *exec.ExitError
	if asExitError(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func (m *Manager) emit(uid int) {
	if m.OnUpdate == nil {
		return
	}
	m.mu.Lock()
	job := m.jobs[uid]
	var snapshot format.Job
	if job != nil {
		snapshot = *job
	}
	m.mu.Unlock()
	m.OnUpdate(snapshot)
}

// Cancel terminates a running job and marks it cancelled. It reports whether
// the job was actually running.
func (m *Manager) Cancel(uid int) bool {
	m.mu.Lock()
	cancel, running := m.running[uid]
	job := m.jobs[uid]
	if running && job != nil {
		job.Status = format.Cancelled
	}
	m.mu.Unlock()
	if !running {
		return false
	}
	cancel()
	m.emit(uid)
	return true
}

// KillAll terminates every running job, used when the app exits.
func (m *Manager) KillAll() {
	m.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(m.running))
	for _, cancel := range m.running {
		cancels = append(cancels, cancel)
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// Wait blocks until every submitted job finished.
func (m *Manager) Wait() { m.wg.Wait() }

// Busy reports whether anything is running or queued.
func (m *Manager) Busy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.running) > 0 || len(m.queued) > 0
}

// RunningCount is how many jobs hold a slot right now.
func (m *Manager) RunningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.running)
}

// MaxObserved is the peak concurrency seen, used by the tests.
func (m *Manager) MaxObserved() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.maxObserved
}

// Snapshot returns a copy of a job's current state.
func (m *Manager) Snapshot(uid int) (format.Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[uid]
	if !ok {
		return format.Job{}, false
	}
	return *job, true
}

// asExitError unwraps an error chain looking for *exec.ExitError.
func asExitError(err error, target **exec.ExitError) bool {
	for err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			*target = exit
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}
