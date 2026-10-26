package ytdlp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ExeSuffix is the platform's executable suffix.
func ExeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// YtdlpName and DenoName are the file names inside the managed directory.
func YtdlpName() string { return "yt-dlp" + ExeSuffix() }
func DenoName() string  { return "deno" + ExeSuffix() }

// CacheDir is where ytd keeps the binaries it manages itself.
func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ytd"), nil
}

// ManagedBinDir is the directory holding the managed yt-dlp and deno.
func ManagedBinDir() (string, error) {
	dir, err := CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bin"), nil
}

// Resolution says how yt-dlp will be invoked.
type Resolution struct {
	Prefix []string
	Origin string
}

// Resolve picks yt-dlp in priority order: an explicit YTD_YTDLP, the user's
// own copy on PATH, then the copy ytd manages in its cache directory.
func Resolve() (Resolution, error) {
	if override := strings.TrimSpace(os.Getenv("YTD_YTDLP")); override != "" {
		return Resolution{Prefix: []string{override}, Origin: "YTD_YTDLP=" + override}, nil
	}
	if found, err := exec.LookPath("yt-dlp"); err == nil {
		return Resolution{Prefix: []string{found}, Origin: "PATH (" + found + ")"}, nil
	}
	if dir, err := ManagedBinDir(); err == nil {
		managed := filepath.Join(dir, YtdlpName())
		if info, err := os.Stat(managed); err == nil && !info.IsDir() {
			return Resolution{Prefix: []string{managed}, Origin: "managed (" + managed + ")"}, nil
		}
	}
	return Resolution{}, fmt.Errorf(
		"yt-dlp not found: install it, set YTD_YTDLP, or run `ytd --update` to fetch a managed copy")
}

// Version reports yt-dlp's version, or "unknown".
func Version(prefix []string) string {
	if len(prefix) == 0 {
		return "unknown"
	}
	args := append(append([]string{}, prefix...), "--version")
	output, err := exec.Command(args[0], args[1:]...).Output()
	if err != nil {
		return "unknown"
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return "unknown"
	}
	return strings.TrimSpace(lines[0])
}

// FFmpegPath is the ffmpeg on PATH, empty when missing.
func FFmpegPath() string {
	found, err := exec.LookPath("ffmpeg")
	if err != nil {
		return ""
	}
	return found
}

// JSRuntime returns a JavaScript runtime yt-dlp can use, checking the given
// extra directories before PATH. YouTube exposes no formats without one.
func JSRuntime(extraDirs []string) string {
	for _, name := range []string{"deno", "node", "bun", "qjs"} {
		if found, err := exec.LookPath(name); err == nil {
			return found
		}
		for _, dir := range extraDirs {
			if dir == "" {
				continue
			}
			candidate := filepath.Join(dir, name+ExeSuffix())
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
		}
	}
	return ""
}

// managedBinDirs is the extra PATH entry the child needs so the managed deno
// is visible when yt-dlp runs.
func managedBinDirs() []string {
	dir, err := ManagedBinDir()
	if err != nil {
		return nil
	}
	return []string{dir}
}

// ChildEnv builds the environment for a yt-dlp child: the parent's, with the
// managed bin directory first on PATH.
func ChildEnv(extraDirs []string) []string {
	sep := string(os.PathListSeparator)
	dirs := append(append([]string{}, extraDirs...), managedBinDirs()...)
	env := os.Environ()
	for index, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		if !found || !strings.EqualFold(key, "PATH") {
			continue
		}
		parts := make([]string, 0, len(dirs)+1)
		for _, dir := range dirs {
			if dir != "" {
				parts = append(parts, dir)
			}
		}
		parts = append(parts, value)
		env[index] = key + "=" + strings.Join(parts, sep)
		return env
	}
	prefix := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if dir != "" {
			prefix = append(prefix, dir)
		}
	}
	if len(prefix) > 0 {
		env = append(env, "PATH="+strings.Join(prefix, sep))
	}
	return env
}

// Update downloads a managed yt-dlp and, when no runtime is present, deno.
// It verifies both against the hashes published with their releases.
func Update(ctx context.Context, client *http.Client, logf func(string)) error {
	dir, err := ManagedBinDir()
	if err != nil {
		return err
	}
	if logf == nil {
		logf = func(string) {}
	}

	needYtdlp := false
	if _, err := Resolve(); err != nil {
		needYtdlp = true
	}
	managedYtdlp := filepath.Join(dir, YtdlpName())
	if _, err := os.Stat(managedYtdlp); err == nil {
		needYtdlp = false
	}
	if os.Getenv("YTD_YTDLP") == "" {
		if _, err := exec.LookPath("yt-dlp"); err != nil {
			if _, err := os.Stat(managedYtdlp); err != nil {
				needYtdlp = true
			}
		}
	}

	if needYtdlp {
		logf("downloading yt-dlp into " + dir)
		release := "https://github.com/yt-dlp/yt-dlp/releases/latest/download/"
		if err := DownloadVerified(ctx, client,
			release+YtdlpName(), release+"SHA2-256SUMS", managedYtdlp); err != nil {
			return fmt.Errorf("yt-dlp: %w", err)
		}
		if err := os.Chmod(managedYtdlp, 0o755); err != nil {
			return err
		}
	}

	if JSRuntime(nil) == "" {
		target, entry, err := denoAsset()
		if err != nil {
			return err
		}
		logf("downloading deno into " + dir)
		release := "https://github.com/denoland/deno/releases/latest/download/"
		archive := "deno-" + target + ".zip"
		dest := filepath.Join(dir, DenoName())
		if err := DownloadZipEntryVerified(ctx, client,
			release+archive, release+archive+".sha256sum", entry, dest); err != nil {
			return fmt.Errorf("deno: %w", err)
		}
		if err := os.Chmod(dest, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func denoAsset() (target, entry string, err error) {
	arch := runtime.GOARCH
	switch runtime.GOOS {
	case "linux":
		if arch != "amd64" && arch != "arm64" {
			return "", "", fmt.Errorf("deno: unsupported architecture %s", arch)
		}
		if arch == "amd64" {
			return "x86_64-unknown-linux-gnu", "deno", nil
		}
		return "aarch64-unknown-linux-gnu", "deno", nil
	case "darwin":
		if arch == "amd64" {
			return "x86_64-apple-darwin", "deno", nil
		}
		return "aarch64-apple-darwin", "deno", nil
	case "windows":
		if arch == "amd64" {
			return "x86_64-pc-windows-msvc", "deno.exe", nil
		}
		return "aarch64-pc-windows-msvc", "deno.exe", nil
	}
	return "", "", fmt.Errorf("deno: unsupported system %s", runtime.GOOS)
}

// Doctor is the data behind `ytd --check`.
type Doctor struct {
	Version   string
	GOOS      string
	GOARCH    string
	Ytdlp     string // origin
	YtdlpVer  string
	FFmpeg    string
	JSRuntime string
	Config    string
	Container string
	OutDir    string
}

// OK reports whether the two hard requirements are present.
func (d Doctor) OK() bool {
	return d.Ytdlp != "missing" && d.FFmpeg != ""
}

// Lines renders the report, one line per fact.
func (d Doctor) Lines() []string {
	value := func(v, missing string) string {
		if v == "" {
			return missing
		}
		return v
	}
	ffmpeg := d.FFmpeg
	if ffmpeg == "" {
		ffmpeg = "MISSING — merging video+audio and embedding will fail"
	}
	runtimeLine := "js runtime " + value(d.JSRuntime, "none — YouTube needs deno or node (run `ytd --update`)")
	if d.JSRuntime != "" {
		runtimeLine = "js runtime " + d.JSRuntime
	}
	return []string{
		fmt.Sprintf("ytd        %s", d.Version),
		fmt.Sprintf("go         %s (%s/%s)", runtime.Version(), d.GOOS, d.GOARCH),
		fmt.Sprintf("yt-dlp     %s", value(d.Ytdlp, "missing")),
		fmt.Sprintf("yt-dlp ver %s", d.YtdlpVer),
		fmt.Sprintf("ffmpeg     %s", ffmpeg),
		runtimeLine,
		fmt.Sprintf("config     %s", value(d.Config, "none found")),
		fmt.Sprintf("container  %s", value(d.Container, "unknown")),
		fmt.Sprintf("outdir     %s", d.OutDir),
	}
}

// Inspect collects the doctor report from the live environment.
func Inspect(version, outDir string) Doctor {
	report := Doctor{Version: version, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, OutDir: outDir}
	resolution, err := Resolve()
	if err != nil {
		report.Ytdlp = "missing"
	} else {
		report.Ytdlp = resolution.Origin
		report.YtdlpVer = Version(resolution.Prefix)
	}
	report.FFmpeg = FFmpegPath()
	report.JSRuntime = JSRuntime(nil)
	env := EnvironMap()
	home := HomeDir()
	if path, ok := ConfigPath(runtime.GOOS, home, env); ok {
		report.Config = path
	}
	report.Container = DetectMergeFormat(runtime.GOOS, home, env)
	return report
}
