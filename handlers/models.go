package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"xkiro-backend/db"
	"xkiro-backend/models"
)

func ModelsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	allModels, err := db.DB.GetAllModels(r.Context())
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
	_ = json.NewEncoder(w).Encode(tiers)
}
