// Package ingest fetches images from upstream waifu APIs, deduplicates
// them by content hash, optimizes for terminal rendering, and stores
// them in the local catalog.
package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Jesssullivan/waifu-mirror/internal/catalog"
	"github.com/Jesssullivan/waifu-mirror/internal/optimize"
)

// Upstream API endpoints.
const (
	waifuImSearchURL  = "https://api.waifu.im/search"
	waifuPicsManyURL  = "https://api.waifu.pics/many/sfw/waifu"
	waifuPicsNSFWURL  = "https://api.waifu.pics/many/nsfw/waifu"
)

// Ingester fetches and processes images from upstream APIs.
type Ingester struct {
	cat    *catalog.DB
	imgDir string
	hc     *http.Client
}

// New creates an Ingester that stores images in imgDir.
func New(cat *catalog.DB, imgDir string) *Ingester {
	return &Ingester{
		cat:    cat,
		imgDir: imgDir,
		hc: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Run performs one ingest cycle: fetches from all upstream sources,
// deduplicates, optimizes, and stores. Returns the count of new images.
func (ing *Ingester) Run(ctx context.Context) (int, error) {
	var total int

	// Fetch SFW from waifu.im
	n, err := ing.ingestWaifuIm(ctx, "sfw")
	if err != nil {
		log.Printf("ingest: waifu.im sfw: %v", err)
	}
	total += n

	// Fetch NSFW from waifu.im
	n, err = ing.ingestWaifuIm(ctx, "nsfw")
	if err != nil {
		log.Printf("ingest: waifu.im nsfw: %v", err)
	}
	total += n

	// Fetch SFW from waifu.pics
	n, err = ing.ingestWaifuPics(ctx, waifuPicsManyURL, "sfw")
	if err != nil {
		log.Printf("ingest: waifu.pics sfw: %v", err)
	}
	total += n

	// Fetch NSFW from waifu.pics
	n, err = ing.ingestWaifuPics(ctx, waifuPicsNSFWURL, "nsfw")
	if err != nil {
		log.Printf("ingest: waifu.pics nsfw: %v", err)
	}
	total += n

	return total, nil
}

// waifuImResponse matches the waifu.im API response structure.
type waifuImResponse struct {
	Images []struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"images"`
}

func (ing *Ingester) ingestWaifuIm(ctx context.Context, category string) (int, error) {
	isNSFW := "false"
	if category == "nsfw" {
		isNSFW = "true"
	}

	url := fmt.Sprintf("%s?included_tags=waifu&is_nsfw=%s&limit=30", waifuImSearchURL, isNSFW)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := ing.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("waifu.im returned %d", resp.StatusCode)
	}

	var result waifuImResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	var count int
	for _, img := range result.Images {
		n, err := ing.processImage(ctx, img.URL, "waifu.im", category, img.Width, img.Height)
		if err != nil {
			log.Printf("ingest: process %s: %v", img.URL, err)
			continue
		}
		count += n
	}
	return count, nil
}

// waifuPicsResponse matches the waifu.pics /many endpoint.
type waifuPicsResponse struct {
	Files []string `json:"files"`
}

func (ing *Ingester) ingestWaifuPics(ctx context.Context, apiURL, category string) (int, error) {
	// waifu.pics /many endpoint expects POST with empty JSON body.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ing.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("waifu.pics returned %d", resp.StatusCode)
	}

	var result waifuPicsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	var count int
	for _, url := range result.Files {
		n, err := ing.processImage(ctx, url, "waifu.pics", category, 0, 0)
		if err != nil {
			log.Printf("ingest: process %s: %v", url, err)
			continue
		}
		count += n
	}
	return count, nil
}

// processImage downloads, deduplicates, optimizes, and stores a single image.
// Returns 1 if the image was new and stored, 0 if duplicate.
func (ing *Ingester) processImage(ctx context.Context, srcURL, source, category string, origW, origH int) (int, error) {
	// Download.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srcURL, nil)
	if err != nil {
		return 0, err
	}

	resp, err := ing.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download %d", resp.StatusCode)
	}

	// Read with 10MB limit.
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return 0, err
	}

	// Content hash for dedup.
	hash := contentHash(data)

	exists, err := ing.cat.HasHash(hash)
	if err != nil {
		return 0, err
	}
	if exists {
		return 0, nil // Already have this image.
	}

	// Optimize for terminal rendering.
	optimized, w, h, err := optimize.ForTerminal(data, 480)
	if err != nil {
		// If optimization fails, use original data.
		optimized = data
		w, h = origW, origH
	}

	// Write to disk.
	filename := hash + ".webp"
	path := filepath.Join(ing.imgDir, filename)
	if err := os.WriteFile(path, optimized, 0o644); err != nil {
		return 0, fmt.Errorf("write image: %w", err)
	}

	// Insert into catalog.
	img := &catalog.Image{
		Hash:      hash,
		Source:    source,
		SourceURL: srcURL,
		Category:  category,
		Width:     w,
		Height:    h,
		Format:    "webp",
		SizeBytes: int64(len(optimized)),
		Filename:  filename,
	}
	if _, err := ing.cat.Insert(img); err != nil {
		os.Remove(path) // Clean up on catalog failure.
		return 0, err
	}

	return 1, nil
}

func contentHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:16])
}
