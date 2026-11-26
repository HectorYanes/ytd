package ytdlp

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// ConfigCandidates lists the config files yt-dlp itself checks, most specific
// first, for the given GOOS name ("windows", "darwin", "linux", ...).
func ConfigCandidates(goos, home string, environ map[string]string) []string {
	env := func(key string) string { return environ[key] }
	var out []string
	switch goos {
	case "windows":
		if appdata := env("APPDATA"); appdata != "" {
			out = append(out, filepath.Join(appdata, "yt-dlp", "config"))
			out = append(out, filepath.Join(appdata, "yt-dlp", "config.txt"))
		}
		out = append(out, filepath.Join(home, "yt-dlp.conf"))
	case "darwin":
		base := filepath.Join(home, "Library", "Application Support", "yt-dlp")
		out = append(out, filepath.Join(base, "config"))
		out = append(out, filepath.Join(base, "config.txt"))
		out = append(out, filepath.Join(home, "yt-dlp.conf"))
	default:
		xdg := env("XDG_CONFIG_HOME")
		if xdg == "" {
			xdg = filepath.Join(home, ".config")
		}
		out = append(out, filepath.Join(xdg, "yt-dlp", "config"))
		out = append(out, filepath.Join(xdg, "yt-dlp", "config.txt"))
		out = append(out, filepath.Join(home, "yt-dlp.conf"))
		out = append(out, "/etc/yt-dlp.conf")
		out = append(out, "/etc/yt-dlp/config")
	}
	return out
}

// ConfigPath returns the first config file that exists.
func ConfigPath(goos, home string, environ map[string]string) (string, bool) {
	for _, path := range ConfigCandidates(goos, home, environ) {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path, true
		}
	}
	return "", false
}

var mergeFormatFlags = []string{"merge-output-format", "remux-video"}

// ParseMergeFormat reads --merge-output-format (or --remux-video) from a config.
func ParseMergeFormat(text string) string {
	for _, flag := range mergeFormatFlags {
		pattern := regexp.MustCompile(`(?m)^\s*--` + flag + `[=\s]+(\w+)`)
		if match := pattern.FindStringSubmatch(text); match != nil {
			return strings.ToLower(match[1])
		}
	}
	return ""
}

// DetectMergeFormat is a best-effort read of the merge container from the
// user's own yt-dlp config.
func DetectMergeFormat(goos, home string, environ map[string]string) string {
	path, ok := ConfigPath(goos, home, environ)
	if !ok {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return ParseMergeFormat(string(data))
}

// EnvironMap turns os.Environ into a map, for the functions above.
func EnvironMap() map[string]string {
	out := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, found := strings.Cut(entry, "=")
		if found {
			out[key] = value
		}
	}
	return out
}

// HomeDir is os.UserHomeDir with a fallback, kept in one place for tests.
func HomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

// DetectUserMergeFormat reads the merge container from this machine's config.
func DetectUserMergeFormat() string {
	return DetectMergeFormat(runtime.GOOS, HomeDir(), EnvironMap())
}
