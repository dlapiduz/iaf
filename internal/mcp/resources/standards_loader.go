package resources

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxStandardsFileSize = 1 << 20 // 1 MB

// loadStandards returns the contents of the standards file at path, or the
// embedded default if path is empty. Returns an error only on I/O failures.
func loadStandards(path string, embedded []byte) ([]byte, error) {
	if path == "" {
		return embedded, nil
	}

	clean := filepath.Clean(path)
	if strings.Contains(clean, "..") {
		return nil, fmt.Errorf("standards file path must not contain traversal sequences")
	}

	f, err := os.Open(clean)
	if err != nil {
		if os.IsNotExist(err) {
			return embedded, nil
		}
		return nil, fmt.Errorf("opening standards file %q: %w", clean, err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxStandardsFileSize))
	if err != nil {
		return nil, fmt.Errorf("reading standards file %q: %w", clean, err)
	}
	return data, nil
}
