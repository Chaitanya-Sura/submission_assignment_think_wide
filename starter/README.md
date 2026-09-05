# Campaign Events Ingestion & Analytics Service

A high-performance, resilient Go service that ingests marketing campaign webhook events from delivery providers and exposes real-time aggregate statistics for marketer dashboards.

---

## Features

- **Robust Partial-Batch Ingestion (`POST /events`):** Accepts JSON arrays of events. Validates each element independently so malformed records (e.g. corrupted timestamps, missing fields) are isolated and rejected without failing or dropping valid records in the same payload.
- **Global Deduplication & Idempotency:** Providers frequently retry events. The service deduplicates by `event_id`, acknowledging duplicate submissions without double-counting metrics.
- **Comprehensive Campaign Analytics (`GET /campaigns/{campaign_id}/stats`):** Computes live volume counters (`sent`, `delivered`, `opened`, `clicked`), contact-isolated unique metrics (`unique_opens`, `unique_clicks`), calculated performance rates (Delivery Rate, Open Rate, Unique Open Rate, CTR, CTOR), and UTC daily delivery/open/click breakdowns.
- **Paginated Event Activity Inspection (`GET /campaigns/{campaign_id}/events`):** Allows inspecting recent campaign activity with `limit`, `offset`, and `type` filtering.
- **Concurrency & Race Safety:** Backed by thread-safe synchronization (`sync.RWMutex`), passing `-race` under high concurrent load.
- **Zero External Dependencies:** Built with pure standard Go library.

---

## Requirements

- Go 1.22 or higher (`go version`)

---

## Running the Service

### Start Server
```bash
cd starter
go run .
```
The server will start listening on port `:8080`.

### Web Dashboard
Open your browser and navigate to:
```
http://localhost:8080/
```
The dashboard provides a real-time interface with live KPI cards, rate progress bars, daily UTC breakdown, interactive event activity log, 1-click seed dataset ingestion, and a webhook batch simulation tool.


---

## API Reference

### 1. Ingest Events
**`POST /events`**

Accepts a JSON array of events.

#### Request Body
```json
[
  {
    "event_id": "evt_00042",
    "campaign_id": "cmp_summer_sale",
    "contact_id": "ct_017",
    "type": "opened",
    "timestamp": "2026-08-10T09:15:04Z",
    "metadata": {"url": "https://example.com/offer"}
  }
]
```

#### Example cURL
```bash
curl -i -X POST http://localhost:8080/events \
  -H "Content-Type: application/json" \
  -d '[
    {"event_id":"evt_1","campaign_id":"cmp_summer_sale","contact_id":"ct_001","type":"sent","timestamp":"2026-08-10T06:00:00Z"},
    {"event_id":"evt_2","campaign_id":"cmp_summer_sale","contact_id":"ct_001","type":"delivered","timestamp":"2026-08-10T06:05:00Z"},
    {"event_id":"evt_3","campaign_id":"cmp_summer_sale","contact_id":"ct_001","type":"opened","timestamp":"2026-08-10T07:15:00Z"}
  ]'
```

#### Response (`200 OK` or `202 Accepted` on partial failures)
```json
{
  "total": 3,
  "accepted": 3,
  "duplicates": 0,
  "rejected": 0
}
```

---

### 2. Ingest Seed Dataset
To ingest the realistic 1-day provider seed file (`seed/events.json`):
```bash
curl -i -X POST http://localhost:8080/events \
  -H "Content-Type: application/json" \
  --data-binary @seed/events.json
```

---

### 3. Get Campaign Statistics
**`GET /campaigns/{campaign_id}/stats`**

#### Example cURL
```bash
curl -s http://localhost:8080/campaigns/cmp_summer_sale/stats
```

#### Response (`200 OK`)
```json
{
  "campaign_id": "cmp_summer_sale",
  "sent": 100,
  "delivered": 95,
  "opened": 60,
  "clicked": 20,
  "unique_opens": 45,
  "unique_clicks": 18,
  "delivery_rate": 0.95,
  "open_rate": 0.6316,
  "unique_open_rate": 0.4737,
  "click_through_rate": 0.2105,
  "click_to_open_rate": 0.3333,
  "daily_stats": {
    "2026-08-10": {
      "date": "2026-08-10",
      "delivered": 95,
      "opened": 60,
      "clicked": 20
    }
  }
}
```

---

### 4. Get Paginated Campaign Events
**`GET /campaigns/{campaign_id}/events?limit=50&offset=0&type=opened`**

#### Example cURL
```bash
curl -s "http://localhost:8080/campaigns/cmp_summer_sale/events?limit=10&offset=0&type=opened"
```

#### Response (`200 OK`)
```json
{
  "campaign_id": "cmp_summer_sale",
  "total": 60,
  "limit": 10,
  "offset": 0,
  "events": [
    {
      "event_id": "evt_00042",
      "campaign_id": "cmp_summer_sale",
      "contact_id": "ct_017",
      "type": "opened",
      "timestamp": "2026-08-10T09:15:04Z"
    }
  ]
}
```

---

## Running Automated Tests

```bash
cd starter
go test -v -race ./...
```
