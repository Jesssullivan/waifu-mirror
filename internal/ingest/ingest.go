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
	"bytes"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Jesssullivan/waifu-mirror/internal/catalog"
	"github.com/Jesssullivan/waifu-mirror/internal/optimize"
	"golang.org/x/time/rate"
)

// Upstream API endpoints.
const (
	waifuImSearchURL = "https://api.waifu.im/images"
	waifuPicsManyURL = "https://api.waifu.pics/many/sfw/waifu"
	waifuPicsNSFWURL = "https://api.waifu.pics/many/nsfw/waifu"
)

// Ingester fetches and processes images from upstream APIs.
type Ingester struct {
	cat    *catalog.DB
	imgDir string
	hc     *http.Client

	// Per-source rate limiters.
	waifuImLimiter   *rate.Limiter // 5 req/sec (API documented limit)
	waifuPicsLimiter *rate.Limiter // 1 req/sec (undocumented, conservative)
	downloadLimiter  *rate.Limiter // 10 req/sec for image downloads
}

const maxRetries = 3

// New creates an Ingester that stores images in imgDir.
func New(cat *catalog.DB, imgDir string) *Ingester {
	return &Ingester{
		cat:    cat,
		imgDir: imgDir,
		hc: &http.Client{
			Timeout: 30 * time.Second,
		},
		waifuImLimiter:   rate.NewLimiter(rate.Limit(5), 1),
		waifuPicsLimiter: rate.NewLimiter(rate.Limit(1), 1),
		downloadLimiter:  rate.NewLimiter(rate.Limit(10), 3),
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

// waifuImResponse matches the waifu.im /images API response.
type waifuImResponse struct {
	Items []struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"items"`
}

func (ing *Ingester) ingestWaifuIm(ctx context.Context, category string) (int, error) {
	isNSFW := "false"
	if category == "nsfw" {
		isNSFW = "true"
	}

	// Rate limit API calls.
	if err := ing.waifuImLimiter.Wait(ctx); err != nil {
		return 0, err
	}

	url := fmt.Sprintf("%s?included_tags=waifu&is_nsfw=%s&page_size=30", waifuImSearchURL, isNSFW)
	body, err := ing.fetchWithRetry(ctx, http.MethodGet, url, nil, "waifu.im", ing.waifuImLimiter)
	if err != nil {
		return 0, err
	}

	var result waifuImResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	var count int
	for _, img := range result.Items {
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
	// Rate limit API calls.
	if err := ing.waifuPicsLimiter.Wait(ctx); err != nil {
		return 0, err
	}

	reqBody := []byte(`{"exclude":[]}`)
	body, err := ing.fetchWithRetry(ctx, http.MethodPost, apiURL, reqBody, "waifu.pics", ing.waifuPicsLimiter)
	if err != nil {
		return 0, err
	}

	var result waifuPicsResponse
	if err := json.Unmarshal(body, &result); err != nil {
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
	// Rate limit downloads.
	if err := ing.downloadLimiter.Wait(ctx); err != nil {
		return 0, err
	}

	// Download with retry.
	data, err := ing.downloadImage(ctx, srcURL)
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

// downloadImage fetches an image with retry and backoff.
func (ing *Ingester) downloadImage(ctx context.Context, srcURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := backoffDuration(attempt)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srcURL, nil)
		if err != nil {
			return nil, err
		}

		resp, err := ing.hc.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("download %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("download %d", resp.StatusCode)
		}

		data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return data, nil
	}
	return nil, fmt.Errorf("after %d retries: %w", maxRetries, lastErr)
}

// fetchWithRetry performs an HTTP request with exponential backoff retry
// for transient errors (429, 5xx) and rate limiting.
func (ing *Ingester) fetchWithRetry(ctx context.Context, method, url string, reqBody []byte, source string, limiter *rate.Limiter) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := backoffDuration(attempt)
			log.Printf("ingest: %s retry %d after %v", source, attempt, backoff)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			// Re-acquire rate limit token on retry.
			if err := limiter.Wait(ctx); err != nil {
				return nil, err
			}
		}

		var bodyReader io.Reader
		if reqBody != nil {
			bodyReader = bytes.NewReader(reqBody)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return nil, err // Not retryable.
		}
		if reqBody != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")

		resp, err := ing.hc.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("%s returned %d", source, resp.StatusCode)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s returned %d", source, resp.StatusCode)
		}
		if err != nil {
			lastErr = err
			continue
		}

		return body, nil
	}
	return nil, fmt.Errorf("after %d retries: %w", maxRetries, lastErr)
}

// backoffDuration returns exponential backoff with jitter.
func backoffDuration(attempt int) time.Duration {
	base := time.Duration(1<<uint(attempt)) * time.Second // 1s, 2s, 4s
	jitter := time.Duration(rand.Int63n(int64(base / 2)))
	return base + jitter
}

func contentHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:16])
}
