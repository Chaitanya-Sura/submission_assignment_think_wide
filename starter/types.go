package main

import "time"

// RawEvent represents incoming unvalidated event payload from provider.
type RawEvent struct {
	EventID    string            `json:"event_id"`
	CampaignID string            `json:"campaign_id"`
	ContactID  string            `json:"contact_id"`
	Type       string            `json:"type"`
	Timestamp  string            `json:"timestamp"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// Event represents a validated and normalized campaign lifecycle event.
type Event struct {
	EventID    string            `json:"event_id"`
	CampaignID string            `json:"campaign_id"`
	ContactID  string            `json:"contact_id"`
	Type       string            `json:"type"` // sent | delivered | opened | clicked
	Timestamp  time.Time         `json:"timestamp"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// IngestResult captures the ingestion outcome for a batch.
type IngestResult struct {
	Total      int               `json:"total"`
	Accepted   int               `json:"accepted"`
	Duplicates int               `json:"duplicates"`
	Rejected   int               `json:"rejected"`
	Errors     []FieldErrorItem  `json:"errors,omitempty"`
}

// FieldErrorItem records why an individual event in a batch was rejected.
type FieldErrorItem struct {
	Index   int    `json:"index"`
	EventID string `json:"event_id,omitempty"`
	Reason  string `json:"reason"`
}

// DailyStats records UTC daily aggregates for a campaign.
type DailyStats struct {
	Date      string `json:"date"`
	Delivered int    `json:"delivered"`
	Opened    int    `json:"opened"`
	Clicked   int    `json:"clicked"`
}

// CampaignStatsResponse is the dashboard statistics view.
type CampaignStatsResponse struct {
	CampaignID       string                 `json:"campaign_id"`
	Sent             int                    `json:"sent"`
	Delivered        int                    `json:"delivered"`
	Opened           int                    `json:"opened"`
	Clicked          int                    `json:"clicked"`
	UniqueOpens      int                    `json:"unique_opens"`
	UniqueClicks     int                    `json:"unique_clicks"`
	DeliveryRate     float64                `json:"delivery_rate"`       // delivered / sent
	OpenRate         float64                `json:"open_rate"`           // opened / delivered
	UniqueOpenRate   float64                `json:"unique_open_rate"`    // unique_opens / delivered
	ClickThroughRate float64                `json:"click_through_rate"`  // clicked / delivered
	ClickToOpenRate  float64                `json:"click_to_open_rate"`  // clicked / opened
	DailyStats       map[string]*DailyStats `json:"daily_stats"`
}

// EventsListResponse is the paginated response for recent campaign events.
type EventsListResponse struct {
	CampaignID string  `json:"campaign_id"`
	Total      int     `json:"total"`
	Limit      int     `json:"limit"`
	Offset     int     `json:"offset"`
	Events     []Event `json:"events"`
}
