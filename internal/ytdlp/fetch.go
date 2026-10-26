package ytdlp

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ErrChecksum means the downloaded bytes do not match the published hash.
var ErrChecksum = errors.New("checksum mismatch")

// DownloadVerified downloads url into dest after checking it against the hash
// published in sumURL (the `<hash>  <name>` format GitHub releases use).
func DownloadVerified(ctx context.Context, client *http.Client, url, sumURL, dest string) error {
	client = orDefaultClient(client)
	expected, err := fetchChecksum(ctx, client, sumURL, path.Base(url))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	part := dest + ".part"
	if err := downloadTo(ctx, client, url, part); err != nil {
		return err
	}
	if err := verifyFile(part, expected); err != nil {
		os.Remove(part)
		return err
	}
	return os.Rename(part, dest)
}

// DownloadZipEntryVerified verifies the archive, then extracts one entry to
// dest with the executable bit set.
func DownloadZipEntryVerified(ctx context.Context, client *http.Client, url, sumURL, entry, dest string) error {
	client = orDefaultClient(client)
	expected, err := fetchChecksum(ctx, client, sumURL, path.Base(url))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	archive := dest + ".zip.part"
	if err := downloadTo(ctx, client, url, archive); err != nil {
		return err
	}
	defer os.Remove(archive)
	if err := verifyFile(archive, expected); err != nil {
		return err
	}
	return extractZipEntry(archive, entry, dest)
}

func orDefaultClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return http.DefaultClient
}

func fetchChecksum(ctx context.Context, client *http.Client, sumURL, name string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sumURL, nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum list: %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		listed := strings.TrimPrefix(fields[1], "*")
		if listed == name || filepath.Base(listed) == name {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("no checksum for %s in %s", name, sumURL)
}

func downloadTo(ctx context.Context, client *http.Client, url, dest string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, response.Status)
	}
	file, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, response.Body); err != nil {
		file.Close()
		os.Remove(dest)
		return err
	}
	return file.Close()
}

func verifyFile(name, want string) error {
	file, err := os.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if got != want {
		return fmt.Errorf("%w: %s has %s, expected %s", ErrChecksum, name, got, want)
	}
	return nil
}

func extractZipEntry(archive, entry, dest string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if file.Name != entry {
			continue
		}
		source, err := file.Open()
		if err != nil {
			return err
		}
		defer source.Close()
		target, err := os.OpenFile(dest+".part", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(target, source); err != nil {
			target.Close()
			return err
		}
		if err := target.Close(); err != nil {
			return err
		}
		return os.Rename(dest+".part", dest)
	}
	return fmt.Errorf("entry %q not found in %s", entry, archive)
}
