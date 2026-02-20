// Package server implements the HTTP API for the waifu mirror.
//
// Endpoints:
//
//	GET /api/random?category=sfw     Random image metadata
//	GET /api/image/:hash             Serve optimized image bytes
//	GET /api/health                  Service health + catalog stats
package server

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jesssullivan/waifu-mirror/internal/catalog"
)

// New creates an HTTP handler for the waifu mirror API.
func New(cat *catalog.DB, imgDir string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/random", randomHandler(cat))
	mux.HandleFunc("GET /api/image/", imageHandler(cat, imgDir))
	mux.HandleFunc("GET /api/health", healthHandler(cat))

	return mux
}

// randomResponse is the JSON body for GET /api/random.
type randomResponse struct {
	URL    string `json:"url"`
	ID     string `json:"id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Hash   string `json:"hash"`
}

func randomHandler(cat *catalog.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		category := r.URL.Query().Get("category")
		if category == "" {
			category = "sfw"
		}
		if category != "sfw" && category != "nsfw" {
			http.Error(w, "category must be sfw or nsfw", http.StatusBadRequest)
			return
		}

		img, err := cat.Random(category)
		if err != nil {
			log.Printf("random: %v", err)
			http.Error(w, "no images available", http.StatusServiceUnavailable)
			return
		}

		resp := randomResponse{
			URL:    "/api/image/" + img.Hash,
			ID:     img.Filename,
			Width:  img.Width,
			Height: img.Height,
			Hash:   img.Hash,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func imageHandler(cat *catalog.DB, imgDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract hash from path: /api/image/{hash}
		hash := strings.TrimPrefix(r.URL.Path, "/api/image/")
		if hash == "" {
			http.Error(w, "missing image hash", http.StatusBadRequest)
			return
		}

		// Sanitize: only allow hex characters.
		for _, c := range hash {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				http.Error(w, "invalid hash", http.StatusBadRequest)
				return
			}
		}

		// Look for the image file.
		pattern := filepath.Join(imgDir, hash+".*")
		matches, _ := filepath.Glob(pattern)
		if len(matches) == 0 {
			http.NotFound(w, r)
			return
		}

		data, err := os.ReadFile(matches[0])
		if err != nil {
			http.Error(w, "read error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "image/webp")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(data)
	}
}

type healthResponse struct {
	Status    string        `json:"status"`
	SFWCount  int           `json:"sfw_count"`
	NSFWCount int           `json:"nsfw_count"`
	TotalMB   float64       `json:"total_mb"`
}

func healthHandler(cat *catalog.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats, err := cat.Stats()
		if err != nil {
			http.Error(w, "stats error", http.StatusInternalServerError)
			return
		}

		resp := healthResponse{
			Status:    "ok",
			SFWCount:  stats.SFWCount,
			NSFWCount: stats.NSFWCount,
			TotalMB:   float64(stats.TotalBytes) / (1024 * 1024),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
