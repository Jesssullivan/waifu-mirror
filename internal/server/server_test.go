package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jesssullivan/waifu-mirror/internal/catalog"
)

func testSetup(t *testing.T) (*catalog.DB, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := catalog.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	imgDir := filepath.Join(t.TempDir(), "images")
	os.MkdirAll(imgDir, 0o755)

	return db, imgDir
}

func TestHealthEndpoint(t *testing.T) {
	db, imgDir := testSetup(t)
	handler := New(db, imgDir)

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("health returned %d, want 200", w.Code)
	}

	var resp healthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q, want ok", resp.Status)
	}
}

func TestRandomEndpoint_Empty(t *testing.T) {
	db, imgDir := testSetup(t)
	handler := New(db, imgDir)

	req := httptest.NewRequest("GET", "/api/random?category=sfw", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("random on empty catalog returned %d, want 503", w.Code)
	}
}

func TestRandomEndpoint_WithImages(t *testing.T) {
	db, imgDir := testSetup(t)

	// Insert test image.
	db.Insert(&catalog.Image{
		Hash: "testhash", Source: "test", SourceURL: "https://example.com",
		Category: "sfw", Width: 480, Height: 680, Filename: "testhash.webp",
	})

	handler := New(db, imgDir)

	req := httptest.NewRequest("GET", "/api/random?category=sfw", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("random returned %d, want 200", w.Code)
	}

	var resp randomResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode random: %v", err)
	}
	if resp.Hash != "testhash" {
		t.Fatalf("hash = %q, want testhash", resp.Hash)
	}
	if resp.Width != 480 {
		t.Fatalf("width = %d, want 480", resp.Width)
	}
}

func TestRandomEndpoint_BadCategory(t *testing.T) {
	db, imgDir := testSetup(t)
	handler := New(db, imgDir)

	req := httptest.NewRequest("GET", "/api/random?category=invalid", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad category returned %d, want 400", w.Code)
	}
}

func TestImageEndpoint(t *testing.T) {
	db, imgDir := testSetup(t)

	// Write a fake image file.
	imgData := []byte("fake-webp-image-data")
	os.WriteFile(filepath.Join(imgDir, "abc123.webp"), imgData, 0o644)

	db.Insert(&catalog.Image{
		Hash: "abc123", Source: "test", SourceURL: "https://example.com",
		Category: "sfw", Filename: "abc123.webp",
	})

	handler := New(db, imgDir)

	req := httptest.NewRequest("GET", "/api/image/abc123", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("image returned %d, want 200", w.Code)
	}
	if w.Header().Get("Content-Type") != "image/webp" {
		t.Fatalf("content-type = %q, want image/webp", w.Header().Get("Content-Type"))
	}
	if w.Body.String() != string(imgData) {
		t.Fatal("image body mismatch")
	}
}

func TestImageEndpoint_NotFound(t *testing.T) {
	db, imgDir := testSetup(t)
	handler := New(db, imgDir)

	// Use a valid hex hash that doesn't exist on disk.
	req := httptest.NewRequest("GET", "/api/image/deadbeef00112233", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("missing image returned %d, want 404", w.Code)
	}
}

func TestImageEndpoint_InvalidHash(t *testing.T) {
	db, imgDir := testSetup(t)
	handler := New(db, imgDir)

	// Non-hex characters should be rejected.
	req := httptest.NewRequest("GET", "/api/image/ZZZZ_invalid", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid hash returned %d, want 400", w.Code)
	}
}
