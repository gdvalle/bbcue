package selfupdate

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// Release represents a GitHub release with the subset of fields we need.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset represents a GitHub release asset.
type Asset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

const (
	apiBase      = "https://api.github.com/repos/gdvalle/bbcue"
	downloadBase = "https://github.com/gdvalle/bbcue/releases/download"
	userAgent    = "bbcue-self-update"

	connectTimeout        = 15 * time.Second
	responseHeaderTimeout = 15 * time.Second
	apiRequestTimeout     = 30 * time.Second
)

func newHTTPTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{
		Timeout:   connectTimeout,
		KeepAlive: 30 * time.Second,
	}).DialContext
	transport.TLSHandshakeTimeout = connectTimeout
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	return transport
}

var (
	apiHTTPClient = &http.Client{
		Timeout:   apiRequestTimeout,
		Transport: newHTTPTransport(),
	}
	downloadHTTPClient = &http.Client{
		Transport: newHTTPTransport(),
	}
)

// FetchLatestRelease returns the latest GitHub release.
func FetchLatestRelease() (*Release, error) {
	req, err := http.NewRequest(http.MethodGet, apiBase+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := apiHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach github.com: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("GitHub API rate limit exceeded. Try again later, or use --release to specify a release tag")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("parsing GitHub API response: %w", err)
	}
	return &rel, nil
}

// FetchRelease returns a specific GitHub release by tag name.
// The tag should include the "v" prefix (e.g. "v0.0.0-20260517110000-f4f227b").
func FetchRelease(tag string) (*Release, error) {
	url := fmt.Sprintf("%s/releases/tags/%s", apiBase, tag)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := apiHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach github.com: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("GitHub API rate limit exceeded. Try again later")
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("release tag not found: %s", tag)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("parsing GitHub API response: %w", err)
	}
	return &rel, nil
}

// FindAsset returns the asset matching the given name, or nil if not found.
func (r *Release) FindAsset(name string) *Asset {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i]
		}
	}
	return nil
}

// DownloadURL returns the direct download URL for a release asset.
func DownloadURL(tagName, assetName string) string {
	return fmt.Sprintf("%s/%s/%s", downloadBase, tagName, assetName)
}

// DownloadBinary downloads a release asset to the given path, with context
// cancellation support. It verifies the Content-Length against the expected
// size if size > 0.
func DownloadBinary(ctx context.Context, url string, size int64, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := downloadHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach github.com: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("asset not found (platform may not be supported)")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	if size > 0 && resp.ContentLength > 0 && resp.ContentLength != size {
		return fmt.Errorf("size mismatch: expected %d bytes, Content-Length is %d", size, resp.ContentLength)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}

	n, err := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if err != nil {
		os.Remove(destPath)
		return fmt.Errorf("download interrupted: %w", err)
	}
	if closeErr != nil {
		os.Remove(destPath)
		return fmt.Errorf("writing downloaded file: %w", closeErr)
	}

	if size > 0 && n != size {
		os.Remove(destPath)
		return fmt.Errorf("size mismatch: expected %d bytes, downloaded %d", size, n)
	}

	if err := os.Chmod(destPath, 0o755); err != nil {
		os.Remove(destPath)
		return fmt.Errorf("setting permissions on downloaded binary: %w", err)
	}

	return nil
}

// DownloadChecksum attempts to download the .sha256 checksum file for an
// asset. If the file doesn't exist (404), it returns an empty string with
// no error — the caller can warn that integrity verification was skipped.
func DownloadChecksum(ctx context.Context, tagName, assetName string) (string, error) {
	url := fmt.Sprintf("%s/%s/%s.sha256", downloadBase, tagName, assetName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := downloadHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading checksum: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil // No checksum available — not an error.
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum download failed: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return "", fmt.Errorf("reading checksum: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// VerifyChecksum checks whether the given SHA-256 sum matches the expected
// checksum. The checksum may include a filename suffix (e.g.
// "abc123  bbcue-linux-amd64") — only the first hex field is used.
func VerifyChecksum(actual []byte, expected string) error {
	if expected == "" {
		return nil
	}
	// Extract just the hex checksum (first field).
	fields := strings.Fields(expected)
	if len(fields) == 0 {
		return fmt.Errorf("invalid checksum format: %q", expected)
	}
	expectedHex := fields[0]

	actualHex := hex.EncodeToString(actual)
	if !strings.EqualFold(actualHex, expectedHex) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHex, actualHex)
	}
	return nil
}
