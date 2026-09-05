package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
)

func TestIngestBatchAndDeduplication(t *testing.T) {
	store := NewMemoryStore()

	events := []RawEvent{
		{EventID: "e1", CampaignID: "cmp_1", ContactID: "c1", Type: "sent", Timestamp: "2026-08-10T10:00:00Z"},
		{EventID: "e2", CampaignID: "cmp_1", ContactID: "c1", Type: "delivered", Timestamp: "2026-08-10T10:01:00Z"},
		{EventID: "e3", CampaignID: "cmp_1", ContactID: "c1", Type: "opened", Timestamp: "2026-08-10T10:05:00Z"},
		{EventID: "e4", CampaignID: "cmp_1", ContactID: "c1", Type: "opened", Timestamp: "2026-08-10T10:06:00Z"}, // same contact opens again
		{EventID: "e5", CampaignID: "cmp_1", ContactID: "c1", Type: "clicked", Timestamp: "2026-08-10T10:10:00Z"},
	}

	res := store.IngestBatch(events)
	if res.Accepted != 5 || res.Duplicates != 0 || res.Rejected != 0 {
		t.Fatalf("unexpected ingest result: %+v", res)
	}

	// Retry exact same batch -> should be 5 duplicates
	res2 := store.IngestBatch(events)
	if res2.Accepted != 0 || res2.Duplicates != 5 || res2.Rejected != 0 {
		t.Fatalf("expected 5 duplicates on retry, got: %+v", res2)
	}

	// Check stats
	stats, ok := store.GetStats("cmp_1")
	if !ok {
		t.Fatalf("expected campaign cmp_1 to exist")
	}

	if stats.Sent != 1 || stats.Delivered != 1 || stats.Opened != 2 || stats.Clicked != 1 {
		t.Errorf("unexpected counts: sent=%d deliv=%d open=%d click=%d",
			stats.Sent, stats.Delivered, stats.Opened, stats.Clicked)
	}

	if stats.UniqueOpens != 1 {
		t.Errorf("expected 1 unique open, got %d", stats.UniqueOpens)
	}

	if stats.DeliveryRate != 1.0 || stats.OpenRate != 2.0 || stats.UniqueOpenRate != 1.0 {
		t.Errorf("unexpected rates: delivRate=%v, openRate=%v, uniqOpenRate=%v",
			stats.DeliveryRate, stats.OpenRate, stats.UniqueOpenRate)
	}
}

func TestPartialBatchWithErrors(t *testing.T) {
	store := NewMemoryStore()

	events := []RawEvent{
		{EventID: "good_1", CampaignID: "cmp_err", ContactID: "c1", Type: "delivered", Timestamp: "2026-08-10T10:00:00Z"},
		{EventID: "bad_time", CampaignID: "cmp_err", ContactID: "c2", Type: "delivered", Timestamp: "not-a-timestamp"},
		{EventID: "bad_type", CampaignID: "cmp_err", ContactID: "c3", Type: "bounced_maybe", Timestamp: "2026-08-10T10:02:00Z"},
		{EventID: "", CampaignID: "cmp_err", ContactID: "c4", Type: "opened", Timestamp: "2026-08-10T10:03:00Z"},
		{EventID: "good_2", CampaignID: "cmp_err", ContactID: "c5", Type: "OPENED", Timestamp: "2026-08-10T10:04:00Z"}, // uppercase type normalization
	}

	res := store.IngestBatch(events)
	if res.Total != 5 || res.Accepted != 2 || res.Rejected != 3 {
		t.Fatalf("expected 2 accepted and 3 rejected, got: %+v", res)
	}

	stats, ok := store.GetStats("cmp_err")
	if !ok {
		t.Fatalf("expected campaign cmp_err to exist")
	}

	if stats.Delivered != 1 || stats.Opened != 1 {
		t.Errorf("expected 1 delivered and 1 opened, got: %+v", stats)
	}
}

func TestContactCrossCampaignIsolation(t *testing.T) {
	store := NewMemoryStore()

	events := []RawEvent{
		{EventID: "e1", CampaignID: "cmp_A", ContactID: "user_42", Type: "opened", Timestamp: "2026-08-10T10:00:00Z"},
		{EventID: "e2", CampaignID: "cmp_B", ContactID: "user_42", Type: "opened", Timestamp: "2026-08-10T10:01:00Z"},
	}

	store.IngestBatch(events)

	statsA, _ := store.GetStats("cmp_A")
	statsB, _ := store.GetStats("cmp_B")

	if statsA.UniqueOpens != 1 {
		t.Errorf("expected 1 unique open for cmp_A, got %d", statsA.UniqueOpens)
	}
	if statsB.UniqueOpens != 1 {
		t.Errorf("expected 1 unique open for cmp_B, got %d", statsB.UniqueOpens)
	}
}

func TestHttpEndpointsAndSeedFile(t *testing.T) {
	store := NewMemoryStore()
	mux := setupRouter(store)

	seedBytes, err := os.ReadFile("seed/events.json")
	if err != nil {
		t.Fatalf("failed to read seed/events.json: %v", err)
	}

	// 1. Post seed events
	req := httptest.NewRequest("POST", "/events", bytes.NewReader(seedBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusAccepted {
		t.Fatalf("unexpected status code: %d, body: %s", w.Code, w.Body.String())
	}

	var ingestRes IngestResult
	if err := json.Unmarshal(w.Body.Bytes(), &ingestRes); err != nil {
		t.Fatalf("failed to parse ingest response: %v", err)
	}

	if ingestRes.Accepted == 0 {
		t.Fatalf("expected accepted events from seed, got 0")
	}

	// 2. Query stats for cmp_summer_sale
	reqStats := httptest.NewRequest("GET", "/campaigns/cmp_summer_sale/stats", nil)
	wStats := httptest.NewRecorder()
	mux.ServeHTTP(wStats, reqStats)

	if wStats.Code != http.StatusOK {
		t.Fatalf("expected 200 for cmp_summer_sale stats, got %d: %s", wStats.Code, wStats.Body.String())
	}

	var statsRes CampaignStatsResponse
	if err := json.Unmarshal(wStats.Body.Bytes(), &statsRes); err != nil {
		t.Fatalf("failed to parse stats response: %v", err)
	}

	if statsRes.Sent == 0 || statsRes.Delivered == 0 || statsRes.Opened == 0 {
		t.Errorf("stats numbers should be populated: %+v", statsRes)
	}

	// 3. Query paginated events
	reqEvents := httptest.NewRequest("GET", "/campaigns/cmp_summer_sale/events?limit=10&offset=0&type=opened", nil)
	wEvents := httptest.NewRecorder()
	mux.ServeHTTP(wEvents, reqEvents)

	if wEvents.Code != http.StatusOK {
		t.Fatalf("expected 200 for cmp_summer_sale events, got %d: %s", wEvents.Code, wEvents.Body.String())
	}

	var eventsRes EventsListResponse
	if err := json.Unmarshal(wEvents.Body.Bytes(), &eventsRes); err != nil {
		t.Fatalf("failed to parse events response: %v", err)
	}

	if len(eventsRes.Events) > 10 {
		t.Errorf("limit exceeded: got %d items", len(eventsRes.Events))
	}
}

func TestConcurrentIngestAndReads(t *testing.T) {
	store := NewMemoryStore()
	mux := setupRouter(store)

	var wg sync.WaitGroup
	workers := 10
	batchesPerWorker := 20

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for b := 0; b < batchesPerWorker; b++ {
				events := []RawEvent{
					{
						EventID:    "evt_conc_" + string(rune('A'+workerID)) + "_" + string(rune('0'+b)),
						CampaignID: "cmp_concurrent",
						ContactID:  "contact_1",
						Type:       "delivered",
						Timestamp:  "2026-08-10T12:00:00Z",
					},
				}
				body, _ := json.Marshal(events)
				req := httptest.NewRequest("POST", "/events", bytes.NewReader(body))
				wRec := httptest.NewRecorder()
				mux.ServeHTTP(wRec, req)

				reqStats := httptest.NewRequest("GET", "/campaigns/cmp_concurrent/stats", nil)
				wStats := httptest.NewRecorder()
				mux.ServeHTTP(wStats, reqStats)
			}
		}(w)
	}

	wg.Wait()

	stats, ok := store.GetStats("cmp_concurrent")
	if !ok {
		t.Fatalf("expected cmp_concurrent stats to exist")
	}

	expectedCount := workers * batchesPerWorker
	if stats.Delivered != expectedCount {
		t.Errorf("expected %d delivered, got %d", expectedCount, stats.Delivered)
	}
}
