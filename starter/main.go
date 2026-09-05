package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

//go:embed static/*
var staticFS embed.FS

var globalStore = NewMemoryStore()

func main() {
	mux := setupRouter(globalStore)
	addr := ":8080"
	fmt.Printf("Campaign Events Service listening on http://localhost%s\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func setupRouter(store *MemoryStore) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", handlePostEvents(store))
	mux.HandleFunc("GET /campaigns", handleListCampaigns(store))
	mux.HandleFunc("GET /campaigns/{campaignID}/stats", handleGetStats(store))
	mux.HandleFunc("GET /campaigns/{campaignID}/events", handleGetEvents(store))
	mux.HandleFunc("GET /health", handleHealth)

	// Serve embedded static dashboard at root
	staticSub, err := fs.Sub(staticFS, "static")
	if err == nil {
		fileServer := http.FileServer(http.FS(staticSub))
		mux.Handle("GET /", fileServer)
	}

	return mux
}

func handleListCampaigns(store *MemoryStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		campaigns := store.ListCampaigns()
		sort.Strings(campaigns)
		writeJSON(w, http.StatusOK, map[string]any{
			"campaigns": campaigns,
			"total":     len(campaigns),
		})
	}
}


// handlePostEvents ingests a JSON array of events from webhook delivery providers.
func handlePostEvents(store *MemoryStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 50*1024*1024)) // 50MB max limit
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
			return
		}
		defer r.Body.Close()

		trimmed := strings.TrimSpace(string(body))
		if trimmed == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty request body"})
			return
		}

		// Try decoding as array of json.RawMessage first to isolate individual item schema issues
		var rawMessages []json.RawMessage
		if err := json.Unmarshal([]byte(trimmed), &rawMessages); err != nil {
			// If array unmarshal fails, check if single object was posted
			var single RawEvent
			if errSingle := json.Unmarshal([]byte(trimmed), &single); errSingle == nil && single.EventID != "" {
				res := store.IngestBatch([]RawEvent{single})
				status := http.StatusOK
				if res.Rejected > 0 && res.Accepted == 0 {
					status = http.StatusBadRequest
				}
				writeJSON(w, status, res)
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error":   "request body must be a JSON array of events",
				"details": err.Error(),
			})
			return
		}

		rawEvents := make([]RawEvent, 0, len(rawMessages))
		var decodeErrors []FieldErrorItem

		for i, rawMsg := range rawMessages {
			var ev RawEvent
			if err := json.Unmarshal(rawMsg, &ev); err != nil {
				decodeErrors = append(decodeErrors, FieldErrorItem{
					Index:  i,
					Reason: fmt.Sprintf("malformed event json: %v", err),
				})
				// Push empty raw event so store records rejection at correct index
				rawEvents = append(rawEvents, RawEvent{})
			} else {
				rawEvents = append(rawEvents, ev)
			}
		}

		result := store.IngestBatch(rawEvents)

		// Determine status code:
		// 200 OK: all accepted/duplicates
		// 202 Accepted: partial acceptance (some accepted, some rejected)
		// 400 Bad Request: all items in batch rejected
		status := http.StatusOK
		if result.Rejected > 0 {
			if result.Accepted == 0 && result.Duplicates == 0 {
				status = http.StatusBadRequest
			} else {
				status = http.StatusAccepted
			}
		}

		writeJSON(w, status, result)
	}
}

// handleGetStats returns aggregated statistics for one campaign.
func handleGetStats(store *MemoryStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		campaignID := strings.TrimSpace(r.PathValue("campaignID"))
		if campaignID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "campaignID is required"})
			return
		}

		stats, ok := store.GetStats(campaignID)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error":       "campaign not found",
				"campaign_id": campaignID,
			})
			return
		}

		writeJSON(w, http.StatusOK, stats)
	}
}

// handleGetEvents returns a paginated list of events for one campaign.
func handleGetEvents(store *MemoryStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		campaignID := strings.TrimSpace(r.PathValue("campaignID"))
		if campaignID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "campaignID is required"})
			return
		}

		q := r.URL.Query()
		eventType := q.Get("type")

		limit := 50
		if lStr := q.Get("limit"); lStr != "" {
			if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
				limit = l
			}
		}

		offset := 0
		if oStr := q.Get("offset"); oStr != "" {
			if o, err := strconv.Atoi(oStr); err == nil && o >= 0 {
				offset = o
			}
		}

		events, ok := store.GetEvents(campaignID, eventType, limit, offset)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error":       "campaign not found",
				"campaign_id": campaignID,
			})
			return
		}

		writeJSON(w, http.StatusOK, events)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}
