// Package catalog manages the image metadata database using SQLite.
// It tracks ingested images, their content hashes, dimensions, and
// source information for deduplication and retrieval.
package catalog

import (
	"database/sql"
	"fmt"
	"math/rand"
	"time"

	_ "modernc.org/sqlite"
)

// Image represents a single cached image in the catalog.
type Image struct {
	ID        int64     `json:"id"`
	Hash      string    `json:"hash"`
	Source    string    `json:"source"`
	SourceURL string    `json:"source_url"`
	Category  string    `json:"category"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Format    string    `json:"format"`
	SizeBytes int64     `json:"size_bytes"`
	Filename  string    `json:"filename"`
	CreatedAt time.Time `json:"created_at"`
}

// Stats holds catalog statistics for the health endpoint.
type Stats struct {
	SFWCount    int       `json:"sfw_count"`
	NSFWCount   int       `json:"nsfw_count"`
	TotalBytes  int64     `json:"total_bytes"`
	LastIngest  time.Time `json:"last_ingest"`
}

// DB wraps a SQLite database for image catalog operations.
type DB struct {
	db *sql.DB
}

// Open creates or opens the catalog database at the given path.
func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path+"?_journal=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("catalog: open: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("catalog: migrate: %w", err)
	}

	return &DB{db: db}, nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS images (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			hash TEXT UNIQUE NOT NULL,
			source TEXT NOT NULL,
			source_url TEXT NOT NULL,
			category TEXT NOT NULL DEFAULT 'sfw',
			width INTEGER NOT NULL DEFAULT 0,
			height INTEGER NOT NULL DEFAULT 0,
			format TEXT NOT NULL DEFAULT 'webp',
			size_bytes INTEGER NOT NULL DEFAULT 0,
			filename TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_images_category ON images(category);
		CREATE INDEX IF NOT EXISTS idx_images_hash ON images(hash);
	`)
	return err
}

// Insert adds a new image to the catalog. Returns the row ID.
func (d *DB) Insert(img *Image) (int64, error) {
	result, err := d.db.Exec(
		`INSERT OR IGNORE INTO images (hash, source, source_url, category, width, height, format, size_bytes, filename)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		img.Hash, img.Source, img.SourceURL, img.Category,
		img.Width, img.Height, img.Format, img.SizeBytes, img.Filename,
	)
	if err != nil {
		return 0, fmt.Errorf("catalog: insert: %w", err)
	}
	return result.LastInsertId()
}

// HasHash checks if an image with the given content hash already exists.
func (d *DB) HasHash(hash string) (bool, error) {
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM images WHERE hash = ?", hash).Scan(&count)
	return count > 0, err
}

// Random returns a random image from the given category.
func (d *DB) Random(category string) (*Image, error) {
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM images WHERE category = ?", category).Scan(&count)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, fmt.Errorf("catalog: no images in category %q", category)
	}

	offset := rand.Intn(count)
	img := &Image{}
	err = d.db.QueryRow(
		`SELECT id, hash, source, source_url, category, width, height, format, size_bytes, filename, created_at
		 FROM images WHERE category = ? LIMIT 1 OFFSET ?`,
		category, offset,
	).Scan(&img.ID, &img.Hash, &img.Source, &img.SourceURL, &img.Category,
		&img.Width, &img.Height, &img.Format, &img.SizeBytes, &img.Filename, &img.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("catalog: random: %w", err)
	}
	return img, nil
}

// Stats returns catalog statistics.
func (d *DB) Stats() (*Stats, error) {
	s := &Stats{}

	d.db.QueryRow("SELECT COUNT(*) FROM images WHERE category = 'sfw'").Scan(&s.SFWCount)
	d.db.QueryRow("SELECT COUNT(*) FROM images WHERE category = 'nsfw'").Scan(&s.NSFWCount)
	d.db.QueryRow("SELECT COALESCE(SUM(size_bytes), 0) FROM images").Scan(&s.TotalBytes)
	d.db.QueryRow("SELECT COALESCE(MAX(created_at), '1970-01-01') FROM images").Scan(&s.LastIngest)

	return s, nil
}

// Count returns the total number of images.
func (d *DB) Count() (int, error) {
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM images").Scan(&count)
	return count, err
}
