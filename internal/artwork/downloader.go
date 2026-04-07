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

// Downloader manages artwork downloads
type Downloader struct {
	cacheDir    string
	currentPath string
	client      *http.Client
}

// NewDownloader creates a new artwork downloader
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

// Download fetches artwork from URL and saves to cache
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

	// Create a cache key from URL hash
	hash := sha256.Sum256([]byte(url))
	ext := getExtension(url)
	filename := fmt.Sprintf("%x%s", hash[:8], ext)
	cachePath := filepath.Join(d.cacheDir, filename)

	// Check if already cached
	if _, err := os.Stat(cachePath); err == nil {
		log.Printf("Artwork cache hit: %s", cachePath)
		d.currentPath = cachePath
		return cachePath, nil
	}

	// Download
	log.Printf("Downloading artwork: %s", url)
	resp, err := d.client.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download artwork: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("artwork download failed: HTTP %d", resp.StatusCode)
	}

	// Save to file
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

// CurrentPath returns the path to the current artwork
func (d *Downloader) CurrentPath() string {
	return d.currentPath
}

// getExtension extracts file extension from URL
func getExtension(url string) string {
	// Remove query string
	url = strings.Split(url, "?")[0]

	// Get extension
	ext := filepath.Ext(url)
	if ext == "" {
		ext = ".jpg" // Default to JPEG
	}

	return ext
}

// Cleanup removes old cached artwork
func (d *Downloader) Cleanup() error {
	return os.RemoveAll(d.cacheDir)
}
