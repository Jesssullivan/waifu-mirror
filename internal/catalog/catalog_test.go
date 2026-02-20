package catalog

import (
	"path/filepath"
	"testing"
)

func testDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestInsertAndHasHash(t *testing.T) {
	db := testDB(t)

	img := &Image{
		Hash:      "abc123",
		Source:    "waifu.im",
		SourceURL: "https://example.com/img.webp",
		Category:  "sfw",
		Width:     480,
		Height:    680,
		Format:    "webp",
		SizeBytes: 50000,
		Filename:  "abc123.webp",
	}

	id, err := db.Insert(img)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive ID, got %d", id)
	}

	has, err := db.HasHash("abc123")
	if err != nil {
		t.Fatalf("HasHash: %v", err)
	}
	if !has {
		t.Fatal("expected HasHash to return true")
	}

	has, err = db.HasHash("nonexistent")
	if err != nil {
		t.Fatalf("HasHash (nonexistent): %v", err)
	}
	if has {
		t.Fatal("expected HasHash to return false for nonexistent hash")
	}
}

func TestInsertDuplicate(t *testing.T) {
	db := testDB(t)

	img := &Image{
		Hash:      "dup123",
		Source:    "waifu.im",
		SourceURL: "https://example.com/dup.webp",
		Category:  "sfw",
		Filename:  "dup123.webp",
	}

	_, err := db.Insert(img)
	if err != nil {
		t.Fatalf("first Insert: %v", err)
	}

	// Second insert with same hash should be ignored (INSERT OR IGNORE).
	_, err = db.Insert(img)
	if err != nil {
		t.Fatalf("duplicate Insert: %v", err)
	}

	count, err := db.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 image after duplicate insert, got %d", count)
	}
}

func TestRandom(t *testing.T) {
	db := testDB(t)

	// Empty catalog should error.
	_, err := db.Random("sfw")
	if err == nil {
		t.Fatal("expected error on empty catalog")
	}

	// Insert some images.
	for i := 0; i < 5; i++ {
		img := &Image{
			Hash:      string(rune('a'+i)) + "hash",
			Source:    "test",
			SourceURL: "https://example.com/test",
			Category:  "sfw",
			Filename:  string(rune('a'+i)) + "hash.webp",
		}
		if _, err := db.Insert(img); err != nil {
			t.Fatalf("Insert #%d: %v", i, err)
		}
	}

	// Random should return something.
	img, err := db.Random("sfw")
	if err != nil {
		t.Fatalf("Random: %v", err)
	}
	if img.Hash == "" {
		t.Fatal("Random returned image with empty hash")
	}
	if img.Category != "sfw" {
		t.Fatalf("Random returned wrong category: %s", img.Category)
	}

	// NSFW category should still be empty.
	_, err = db.Random("nsfw")
	if err == nil {
		t.Fatal("expected error for empty nsfw category")
	}
}

func TestStats(t *testing.T) {
	db := testDB(t)

	// Insert SFW and NSFW images.
	for i := 0; i < 3; i++ {
		db.Insert(&Image{
			Hash: string(rune('a'+i)) + "sfw", Source: "test", SourceURL: "u",
			Category: "sfw", Filename: "f.webp", SizeBytes: 1000,
		})
	}
	for i := 0; i < 2; i++ {
		db.Insert(&Image{
			Hash: string(rune('a'+i)) + "nsfw", Source: "test", SourceURL: "u",
			Category: "nsfw", Filename: "f.webp", SizeBytes: 2000,
		})
	}

	stats, err := db.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.SFWCount != 3 {
		t.Fatalf("SFWCount = %d, want 3", stats.SFWCount)
	}
	if stats.NSFWCount != 2 {
		t.Fatalf("NSFWCount = %d, want 2", stats.NSFWCount)
	}
	if stats.TotalBytes != 7000 {
		t.Fatalf("TotalBytes = %d, want 7000", stats.TotalBytes)
	}
}

func TestCount(t *testing.T) {
	db := testDB(t)

	count, err := db.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}

	db.Insert(&Image{
		Hash: "x", Source: "test", SourceURL: "u", Category: "sfw", Filename: "f.webp",
	})

	count, err = db.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1, got %d", count)
	}
}
