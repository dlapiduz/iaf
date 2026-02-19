package sourcestore

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStoreFiles_AndServe(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir, "http://localhost:8080", slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	files := map[string]string{
		"main.go": "package main\nfunc main() {}\n",
		"go.mod":  "module test\ngo 1.22\n",
	}

	blobURL, err := store.StoreFiles("test-ns", "myapp", files)
	if err != nil {
		t.Fatal(err)
	}

	if blobURL != "http://localhost:8080/sources/test-ns/myapp/source.tar.gz" {
		t.Errorf("unexpected blob URL: %s", blobURL)
	}

	// Simulate the HTTP serving chain as mounted in the apiserver:
	// e.GET("/sources/*", echo.WrapHandler(http.StripPrefix("/sources/", store.Handler())))
	handler := http.StripPrefix("/sources/", store.Handler())

	req := httptest.NewRequest("GET", "/sources/test-ns/myapp/source.tar.gz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Body.Len() == 0 {
		t.Error("expected non-empty response body")
	}
}

func TestStoreFiles_ServeMissing(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir, "http://localhost:8080", slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	handler := http.StripPrefix("/sources/", store.Handler())
	req := httptest.NewRequest("GET", "/sources/nonexistent/source.tar.gz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestStoreFiles_Delete(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir, "http://localhost:8080", slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.StoreFiles("test-ns", "myapp", map[string]string{"f.txt": "hello"})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Delete("test-ns", "myapp"); err != nil {
		t.Fatal(err)
	}

	handler := http.StripPrefix("/sources/", store.Handler())
	req := httptest.NewRequest("GET", "/sources/test-ns/myapp/source.tar.gz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", w.Code)
	}
}

func TestStoreFiles_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir, "http://localhost:8080", slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.StoreFiles("test-ns", "myapp", map[string]string{
		"../etc/passwd": "bad",
	})
	if err == nil {
		t.Error("expected error for path traversal")
	}
}
