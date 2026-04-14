// ABOUTME: Artwork downloader for album art and metadata images
// ABOUTME: Downloads images from URLs and saves to temp directory
package artwork

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Downloader struct {
	cacheDir    string
	currentPath string
	client      *http.Client
}

func NewDownloader() (*Downloader, error) {
	cacheDir := filepath.Join(os.TempDir(), "resonate-artwork")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	return &Downloader{
		cacheDir: cacheDir,
		client:   &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (d *Downloader) Download(url string) (string, error) {
	if url == "" {
		return "", nil
	}

	// Validate URL scheme to prevent SSRF
	parsed, err := neturl.Parse(url)
	if err != nil {
		return "", fmt.Errorf("invalid artwork URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme %q: only http and https are allowed", parsed.Scheme)
	}

	hash := sha256.Sum256([]byte(url))
	ext := getExtension(url)
	filename := fmt.Sprintf("%x%s", hash[:8], ext)
	cachePath := filepath.Join(d.cacheDir, filename)

	if _, err := os.Stat(cachePath); err == nil {
		log.Printf("Artwork cache hit: %s", cachePath)
		d.currentPath = cachePath
		return cachePath, nil
	}

	log.Printf("Downloading artwork: %s", url)
	resp, err := d.client.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download artwork: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("artwork download failed: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(cachePath)
	if err != nil {
		return "", fmt.Errorf("failed to create cache file: %w", err)
	}
	defer f.Close()

	// Limit response body to 10MB to prevent resource exhaustion
	limitedBody := io.LimitReader(resp.Body, 10*1024*1024)

	if _, err := io.Copy(f, limitedBody); err != nil {
		os.Remove(cachePath)
		return "", fmt.Errorf("failed to save artwork: %w", err)
	}

	log.Printf("Artwork saved: %s", cachePath)
	d.currentPath = cachePath
	return cachePath, nil
}

func (d *Downloader) CurrentPath() string {
	return d.currentPath
}

func getExtension(url string) string {
	url = strings.Split(url, "?")[0]

	ext := filepath.Ext(url)
	if ext == "" {
		ext = ".jpg" // Default to JPEG
	}

	return ext
}

func (d *Downloader) Cleanup() error {
	return os.RemoveAll(d.cacheDir)
}
