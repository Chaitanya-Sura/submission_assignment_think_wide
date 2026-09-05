package main

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// Valid event types
const (
	TypeSent      = "sent"
	TypeDelivered = "delivered"
	TypeOpened    = "opened"
	TypeClicked   = "clicked"
)

// CampaignState holds raw counters and lookup sets for one campaign.
type CampaignState struct {
	Sent          int
	Delivered     int
	Opened        int
	Clicked       int
	UniqueOpens   map[string]bool // contact_id -> true
	UniqueClicks  map[string]bool // contact_id -> true
	DailyStats    map[string]*DailyStats
	Events        []Event
}

// MemoryStore manages campaign state and deduplication with mutex protection.
type MemoryStore struct {
	mu        sync.RWMutex
	seenIDs   map[string]bool // global event_id deduplication
	campaigns map[string]*CampaignState
}

// NewMemoryStore initializes an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		seenIDs:   make(map[string]bool),
		campaigns: make(map[string]*CampaignState),
	}
}

// IngestBatch processes a batch of raw events, validating each item independently.
func (s *MemoryStore) IngestBatch(rawEvents []RawEvent) IngestResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	res := IngestResult{
		Total:  len(rawEvents),
		Errors: make([]FieldErrorItem, 0),
	}

	for i, raw := range rawEvents {
		event, err := validateAndNormalize(raw)
		if err != nil {
			res.Rejected++
			res.Errors = append(res.Errors, FieldErrorItem{
				Index:   i,
				EventID: raw.EventID,
				Reason:  err.Error(),
			})
			continue
		}

		if s.seenIDs[event.EventID] {
			res.Duplicates++
			continue
		}

		s.seenIDs[event.EventID] = true
		res.Accepted++

		camp, ok := s.campaigns[event.CampaignID]
		if !ok {
			camp = &CampaignState{
				UniqueOpens:  make(map[string]bool),
				UniqueClicks: make(map[string]bool),
				DailyStats:   make(map[string]*DailyStats),
				Events:       make([]Event, 0),
			}
			s.campaigns[event.CampaignID] = camp
		}

		camp.Events = append(camp.Events, event)

		dateKey := event.Timestamp.UTC().Format("2006-01-02")
		daily, ok := camp.DailyStats[dateKey]
		if !ok {
			daily = &DailyStats{Date: dateKey}
			camp.DailyStats[dateKey] = daily
		}

		switch event.Type {
		case TypeSent:
			camp.Sent++
		case TypeDelivered:
			camp.Delivered++
			daily.Delivered++
		case TypeOpened:
			camp.Opened++
			daily.Opened++
			camp.UniqueOpens[event.ContactID] = true
		case TypeClicked:
			camp.Clicked++
			daily.Clicked++
			camp.UniqueClicks[event.ContactID] = true
		}
	}

	return res
}

// GetStats returns aggregated metrics and calculated rates for a campaign.
func (s *MemoryStore) GetStats(campaignID string) (*CampaignStatsResponse, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	camp, ok := s.campaigns[campaignID]
	if !ok {
		return nil, false
	}

	uniqueOpensCount := len(camp.UniqueOpens)
	uniqueClicksCount := len(camp.UniqueClicks)

	dailyCopy := make(map[string]*DailyStats, len(camp.DailyStats))
	for k, v := range camp.DailyStats {
		dailyCopy[k] = &DailyStats{
			Date:      v.Date,
			Delivered: v.Delivered,
			Opened:    v.Opened,
			Clicked:   v.Clicked,
		}
	}

	var deliveryRate, openRate, uniqueOpenRate, ctr, ctor float64
	if camp.Sent > 0 {
		deliveryRate = roundRate(float64(camp.Delivered) / float64(camp.Sent))
	}
	if camp.Delivered > 0 {
		openRate = roundRate(float64(camp.Opened) / float64(camp.Delivered))
		uniqueOpenRate = roundRate(float64(uniqueOpensCount) / float64(camp.Delivered))
		ctr = roundRate(float64(camp.Clicked) / float64(camp.Delivered))
	}
	if camp.Opened > 0 {
		ctor = roundRate(float64(camp.Clicked) / float64(camp.Opened))
	}

	return &CampaignStatsResponse{
		CampaignID:       campaignID,
		Sent:             camp.Sent,
		Delivered:        camp.Delivered,
		Opened:           camp.Opened,
		Clicked:          camp.Clicked,
		UniqueOpens:      uniqueOpensCount,
		UniqueClicks:     uniqueClicksCount,
		DeliveryRate:     deliveryRate,
		OpenRate:         openRate,
		UniqueOpenRate:   uniqueOpenRate,
		ClickThroughRate: ctr,
		ClickToOpenRate:  ctor,
		DailyStats:       dailyCopy,
	}, true
}

// GetEvents returns a paginated slice of campaign events with optional type filtering.
func (s *MemoryStore) GetEvents(campaignID string, eventType string, limit, offset int) (*EventsListResponse, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	camp, ok := s.campaigns[campaignID]
	if !ok {
		return nil, false
	}

	var filtered []Event
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	if eventType == "" {
		filtered = camp.Events
	} else {
		filtered = make([]Event, 0, len(camp.Events))
		for _, ev := range camp.Events {
			if ev.Type == eventType {
				filtered = append(filtered, ev)
			}
		}
	}

	total := len(filtered)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	end := offset + limit
	if end > total {
		end = total
	}

	paginated := make([]Event, end-offset)
	copy(paginated, filtered[offset:end])

	return &EventsListResponse{
		CampaignID: campaignID,
		Total:      total,
		Limit:      limit,
		Offset:     offset,
		Events:     paginated,
	}, true
}

// validateAndNormalize parses and validates fields for a raw event.
func validateAndNormalize(raw RawEvent) (Event, error) {
	eventID := strings.TrimSpace(raw.EventID)
	if eventID == "" {
		return Event{}, fmt.Errorf("missing or empty event_id")
	}

	campaignID := strings.TrimSpace(raw.CampaignID)
	if campaignID == "" {
		return Event{}, fmt.Errorf("missing or empty campaign_id")
	}

	contactID := strings.TrimSpace(raw.ContactID)
	if contactID == "" {
		return Event{}, fmt.Errorf("missing or empty contact_id")
	}

	eventType := strings.ToLower(strings.TrimSpace(raw.Type))
	switch eventType {
	case TypeSent, TypeDelivered, TypeOpened, TypeClicked:
		// Valid type
	default:
		return Event{}, fmt.Errorf("invalid event type '%s'", raw.Type)
	}

	tStr := strings.TrimSpace(raw.Timestamp)
	if tStr == "" {
		return Event{}, fmt.Errorf("missing or empty timestamp")
	}

	// Support RFC3339, RFC3339Nano, ISO8601 variations
	t, err := time.Parse(time.RFC3339, tStr)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, tStr)
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05Z07:00", tStr)
			if err != nil {
				return Event{}, fmt.Errorf("invalid ISO 8601 timestamp '%s'", tStr)
			}
		}
	}

	return Event{
		EventID:    eventID,
		CampaignID: campaignID,
		ContactID:  contactID,
		Type:       eventType,
		Timestamp:  t.UTC(),
		Metadata:   raw.Metadata,
	}, nil
}

func roundRate(val float64) float64 {
	return math.Round(val*10000) / 10000
}
