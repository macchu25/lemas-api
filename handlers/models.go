package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"xkiro-backend/db"
	"xkiro-backend/models"
)

var (
	modelsCacheMu   sync.RWMutex
	cachedModels    []models.ModelItem
	cacheExpiryTime time.Time
)

func getCachedOrFetchModels(r *http.Request) ([]models.ModelItem, error) {
	modelsCacheMu.RLock()
	if len(cachedModels) > 0 && time.Now().Before(cacheExpiryTime) {
		result := cachedModels
		modelsCacheMu.RUnlock()
		return result, nil
	}
	modelsCacheMu.RUnlock()

	modelsCacheMu.Lock()
	defer modelsCacheMu.Unlock()

	// Double-check after acquiring write lock
	if len(cachedModels) > 0 && time.Now().Before(cacheExpiryTime) {
		return cachedModels, nil
	}

	fetched, err := db.DB.GetAllModels(r.Context())
	if err != nil {
		if len(cachedModels) > 0 {
			return cachedModels, nil // Gracefully return stale cache if DB is busy
		}
		return nil, err
	}

	cachedModels = fetched
	cacheExpiryTime = time.Now().Add(5 * time.Minute) // 5 minutes in-memory TTL
	return cachedModels, nil
}

// ModelsHandler: GET /api/models with High-Performance Memory Cache & Edge Headers
func ModelsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	allModels, err := getCachedOrFetchModels(r)
	if err != nil {
		http.Error(w, `{"error":"failed to fetch models"}`, http.StatusInternalServerError)
		return
	}

	query := strings.ToLower(r.URL.Query().Get("q"))
	provider := strings.ToLower(r.URL.Query().Get("provider"))
	freeOnly := r.URL.Query().Get("free") == "true"

	var filtered []models.ModelItem
	for _, m := range allModels {
		if provider != "" && strings.ToLower(m.Provider) != provider {
			continue
		}
		if freeOnly && !m.IsFree {
			continue
		}
		if query != "" {
			match := strings.Contains(strings.ToLower(m.Name), query) ||
				strings.Contains(strings.ToLower(m.ID), query) ||
				strings.Contains(strings.ToLower(m.Provider), query) ||
				strings.Contains(strings.ToLower(m.Description), query)
			if !match {
				continue
			}
		}
		filtered = append(filtered, m)
	}

	if filtered == nil {
		filtered = []models.ModelItem{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=120, stale-while-revalidate=300")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data":   filtered,
		"total":  len(filtered),
	})
}

func PricingTiersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	tiers, err := db.DB.GetPricingTiers(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to get pricing tiers"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300, stale-while-revalidate=600")
	_ = json.NewEncoder(w).Encode(tiers)
}
