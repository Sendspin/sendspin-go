// ABOUTME: Tests for artwork downloader
// ABOUTME: Tests HTTP download, caching, and error handling
package artwork

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestNewDownloader(t *testing.T) {
	dl, err := NewDownloader()
	if err != nil {
		t.Fatalf("failed to create downloader: %v", err)
	}

	if dl == nil {
		t.Fatal("expected downloader to be created")
	}

	if _, err := os.Stat(dl.cacheDir); os.IsNotExist(err) {
		t.Error("cache directory was not created")
	}

	dl.Cleanup()
}

func TestDownloadSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fake image data"))
	}))
	defer server.Close()

	dl, err := NewDownloader()
	if err != nil {
		t.Fatalf("failed to create downloader: %v", err)
	}
	defer dl.Cleanup()

	path, err := dl.Download(server.URL)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	if path == "" {
		t.Fatal("expected path to be returned")
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("artwork file was not created at %s", path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read artwork file: %v", err)
	}

	if string(content) != "fake image data" {
		t.Errorf("expected content 'fake image data', got '%s'", string(content))
	}

	if dl.CurrentPath() != path {
		t.Errorf("expected CurrentPath to be %s, got %s", path, dl.CurrentPath())
	}
}

func TestDownloadCaching(t *testing.T) {
	requestCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fake image data"))
	}))
	defer server.Close()

	dl, err := NewDownloader()
	if err != nil {
		t.Fatalf("failed to create downloader: %v", err)
	}
	defer dl.Cleanup()

	path1, err := dl.Download(server.URL)
	if err != nil {
		t.Fatalf("first download failed: %v", err)
	}

	if requestCount != 1 {
		t.Errorf("expected 1 request, got %d", requestCount)
	}

	// Second download hits the cache; verify server was not called again
	path2, err := dl.Download(server.URL)
	if err != nil {
		t.Fatalf("second download failed: %v", err)
	}

	if requestCount != 1 {
		t.Errorf("expected cached download to not hit server, but got %d requests", requestCount)
	}

	if path1 != path2 {
		t.Errorf("expected same path for cached download, got %s and %s", path1, path2)
	}
}

func TestDownloadHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	dl, err := NewDownloader()
	if err != nil {
		t.Fatalf("failed to create downloader: %v", err)
	}
	defer dl.Cleanup()

	_, err = dl.Download(server.URL)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}

	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected error to mention 404, got: %v", err)
	}
}

func TestDownloadEmptyURL(t *testing.T) {
	dl, err := NewDownloader()
	if err != nil {
		t.Fatalf("failed to create downloader: %v", err)
	}
	defer dl.Cleanup()

	path, err := dl.Download("")
	if err != nil {
		t.Errorf("expected no error for empty URL, got: %v", err)
	}

	if path != "" {
		t.Errorf("expected empty path for empty URL, got: %s", path)
	}
}

func TestDownloadInvalidURL(t *testing.T) {
	dl, err := NewDownloader()
	if err != nil {
		t.Fatalf("failed to create downloader: %v", err)
	}
	defer dl.Cleanup()

	_, err = dl.Download("not-a-valid-url")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestGetExtension(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"http://example.com/image.jpg", ".jpg"},
		{"http://example.com/image.png", ".png"},
		{"http://example.com/image.webp", ".webp"},
		{"http://example.com/image.jpg?size=large", ".jpg"},
		{"http://example.com/image", ".jpg"}, // Default
		{"http://example.com/path/to/image.jpeg", ".jpeg"},
	}

	for _, tt := range tests {
		result := getExtension(tt.url)
		if result != tt.expected {
			t.Errorf("getExtension(%q) = %q, expected %q", tt.url, result, tt.expected)
		}
	}
}

func TestDownloadMultipleURLs(t *testing.T) {
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("image 1"))
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("image 2"))
	}))
	defer server2.Close()

	dl, err := NewDownloader()
	if err != nil {
		t.Fatalf("failed to create downloader: %v", err)
	}
	defer dl.Cleanup()

	path1, err := dl.Download(server1.URL)
	if err != nil {
		t.Fatalf("first download failed: %v", err)
	}

	path2, err := dl.Download(server2.URL)
	if err != nil {
		t.Fatalf("second download failed: %v", err)
	}

	if path1 == path2 {
		t.Error("expected different paths for different URLs")
	}

	if _, err := os.Stat(path1); os.IsNotExist(err) {
		t.Errorf("first file was not created at %s", path1)
	}
	if _, err := os.Stat(path2); os.IsNotExist(err) {
		t.Errorf("second file was not created at %s", path2)
	}

	if dl.CurrentPath() != path2 {
		t.Errorf("expected CurrentPath to be %s, got %s", path2, dl.CurrentPath())
	}
}

func TestCleanup(t *testing.T) {
	dl, err := NewDownloader()
	if err != nil {
		t.Fatalf("failed to create downloader: %v", err)
	}

	cacheDir := dl.cacheDir

	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		t.Fatal("cache directory was not created")
	}

	err = dl.Cleanup()
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Error("cache directory still exists after cleanup")
	}
}

// NOTE: TestConcurrentDownloads was removed because the downloader is called
// sequentially from the player's update loop, not concurrently. If concurrent
// access is needed in the future, proper synchronization should be added.
func TestHashConsistency(t *testing.T) {
	dl, err := NewDownloader()
	if err != nil {
		t.Fatalf("failed to create downloader: %v", err)
	}
	defer dl.Cleanup()

	dl2, _ := NewDownloader()
	defer dl2.Cleanup()

	// Both should use the same cache directory pattern
	if !strings.HasPrefix(dl.cacheDir, os.TempDir()) {
		t.Error("cache directory should be in temp dir")
	}
	if !strings.HasPrefix(dl2.cacheDir, os.TempDir()) {
		t.Error("cache directory should be in temp dir")
	}

	// Both should include "resonate-artwork" in path
	if !strings.Contains(dl.cacheDir, "resonate-artwork") {
		t.Error("cache directory should contain 'resonate-artwork'")
	}
	if !strings.Contains(dl2.cacheDir, "resonate-artwork") {
		t.Error("cache directory should contain 'resonate-artwork'")
	}
}
