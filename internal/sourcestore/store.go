package sourcestore

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Store manages uploaded source code as tarballs and serves them over HTTP.
type Store struct {
	dir     string // directory for storing tarballs
	baseURL string // base URL for serving tarballs
	logger  *slog.Logger
}

// New creates a new source store.
func New(dir, baseURL string, logger *slog.Logger) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating source store directory: %w", err)
	}
	return &Store{
		dir:     dir,
		baseURL: strings.TrimRight(baseURL, "/"),
		logger:  logger,
	}, nil
}

// StoreFiles takes a map of file paths to contents and stores them as a gzipped tarball.
// Returns the blob URL that kpack can fetch.
func (s *Store) StoreFiles(namespace, appName string, files map[string]string) (string, error) {
	appDir := filepath.Join(s.dir, namespace, appName)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", fmt.Errorf("creating app source directory: %w", err)
	}

	tarballPath := filepath.Join(appDir, "source.tar.gz")

	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gzWriter)

	for path, content := range files {
		// Sanitize path to prevent directory traversal
		cleanPath := filepath.Clean(path)
		if strings.HasPrefix(cleanPath, "..") || filepath.IsAbs(cleanPath) {
			return "", fmt.Errorf("invalid file path: %s", path)
		}

		header := &tar.Header{
			Name: cleanPath,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return "", fmt.Errorf("writing tar header for %s: %w", path, err)
		}
		if _, err := tarWriter.Write([]byte(content)); err != nil {
			return "", fmt.Errorf("writing tar content for %s: %w", path, err)
		}
	}

	if err := tarWriter.Close(); err != nil {
		return "", fmt.Errorf("closing tar writer: %w", err)
	}
	if err := gzWriter.Close(); err != nil {
		return "", fmt.Errorf("closing gzip writer: %w", err)
	}

	if err := os.WriteFile(tarballPath, buf.Bytes(), 0o644); err != nil {
		return "", fmt.Errorf("writing tarball: %w", err)
	}

	blobURL := fmt.Sprintf("%s/sources/%s/%s/source.tar.gz", s.baseURL, namespace, appName)
	s.logger.Info("stored source code", "namespace", namespace, "app", appName, "url", blobURL, "files", len(files))
	return blobURL, nil
}

// StoreTarball stores a raw tarball for an application.
// Returns the blob URL.
func (s *Store) StoreTarball(namespace, appName string, r io.Reader) (string, error) {
	appDir := filepath.Join(s.dir, namespace, appName)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", fmt.Errorf("creating app source directory: %w", err)
	}

	tarballPath := filepath.Join(appDir, "source.tar.gz")
	f, err := os.Create(tarballPath)
	if err != nil {
		return "", fmt.Errorf("creating tarball file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		return "", fmt.Errorf("writing tarball: %w", err)
	}

	blobURL := fmt.Sprintf("%s/sources/%s/%s/source.tar.gz", s.baseURL, namespace, appName)
	s.logger.Info("stored source tarball", "namespace", namespace, "app", appName, "url", blobURL)
	return blobURL, nil
}

// Handler returns an HTTP handler that serves source tarballs.
// The caller is responsible for stripping the URL prefix before calling this handler.
func (s *Store) Handler() http.Handler {
	return http.FileServer(http.Dir(s.dir))
}

// Delete removes stored source for an application.
func (s *Store) Delete(namespace, appName string) error {
	appDir := filepath.Join(s.dir, namespace, appName)
	return os.RemoveAll(appDir)
}
