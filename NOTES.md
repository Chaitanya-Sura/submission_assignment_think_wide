# Campaign Events — Architecture, Scale & Operational Notes

## Part 1 — Read & Think First

### 1. Problem Interpretation
Relay acts as an intermediary communication platform that dispatches high volumes of marketing messages across email, SMS, and push channels via external delivery providers. Delivery providers asynchronously notify Relay of message lifecycle milestones (`sent`, `delivered`, `opened`, `clicked`) via batch webhook calls.

Our service is the single source of truth for downstream marketer dashboards. The core technical challenge stems from the inherent realities of distributed webhook pipelines:
- **At-least-once delivery & retries:** Providers retry HTTP requests on timeouts, transient errors, or network hiccups. The same event payload may be posted multiple times.
- **Out-of-order & delayed delivery:** Events do not arrive sequentially. For instance, an `opened` event can arrive before a `delivered` event due to varying provider pipelines or network latencies.
- **Malformed & mixed batches:** Webhook payloads arrive in batches where 99% of events may be valid, but occasional items contain corrupted timestamps or malformed JSON. A robust ingestion pipeline must reject malformed records without failing the entire batch or dropping legitimate events.
- **Accurate business metrics:** Dashboard queries must reflect accurate real-time aggregates (total volume, unique interactions, engagement rates, daily distributions) without duplicate counting.

---

### 2. Assumptions
1. **Event Identity (`event_id`):** An event is uniquely identified by `event_id`. If two payloads share the same `event_id`, they represent the exact same delivery event; duplicates must be recorded idempotently without re-incrementing counters.
2. **Contact Isolation:** `contact_id` represents an individual recipient. A contact opening multiple messages within the *same* campaign counts as one unique open for that campaign. If the same contact opens messages across *two different* campaigns, they count once in each campaign.
3. **UTC Timestamps:** All timestamps are ISO 8601 / RFC 3339 UTC strings. Daily metric aggregations must strictly use UTC calendar dates.
4. **Valid Event Types:** Only four valid lifecycle types exist: `sent`, `delivered`, `opened`, `clicked`. Types are normalized case-insensitively (e.g., `"OPENED"` -> `"opened"`).
5. **Partial Batch Ingestion:** If a batch of 100 events contains 2 malformed records, the 98 valid events should be ingested, and the response should explicitly report accepted, duplicate, and rejected counts alongside error details.

---

### 3. Ambiguities in the Brief
- **Out-of-Order Lifecycle Semantics:** If an `opened` or `clicked` event arrives without a preceding `sent` or `delivered` event (e.g., the delivery webhook was dropped or delayed), should the service increment `opened`/`clicked` immediately, synthesize missing state, or buffer until `delivered` arrives? (We assume immediate processing without synthetic backfills).
- **Dashboard Response Model:** The brief asks for "campaign stats" without specifying exact KPI calculations (e.g., whether open rate is `opened / delivered` or `unique_opens / delivered`, whether CTR is `clicked / delivered` or `clicked / opened`).
- **Batch Endpoint Partial Failure Codes:** Whether partial failures return HTTP 200, 202 (Accepted with errors), or 207 (Multi-Status).
- **Event Retention & History:** Whether raw events need to be stored indefinitely for audit/replay, or if real-time rollups/counters suffice.

---

### 4. Questions for the Product Manager
1. **Metric Definition Alignment:** Does the dashboard define Open Rate as Unique Opens / Delivered (`unique_opens / delivered`) or Gross Opens / Delivered (`opened / delivered`)? How should we handle cases where `delivered == 0` but `opened > 0` due to out-of-order delivery?
2. **Data Retention & Historical Events:** What is the retention policy for raw events? Marketers frequently ask for event logs ("Show me the last 50 events for campaign X"); should this endpoint support full historical drill-downs or just recent events?
3. **Provider Idempotency Guarantees:** Can providers rotate or alter `event_id` during retries, or can we strictly rely on `event_id` as the global deduplication key?
4. **Late-Arriving Data Policy:** If an event arrives 14 days after a campaign ends, should it retroactively update historical daily buckets and campaign totals, or should late events beyond a cutoff window be flagged?

---

### 5. Prioritization Strategy
Given the time budget, priorities were ordered by business criticality and risk:
1. **Resilient Ingestion (`POST /events`):** High throughput, streaming array decoder, resilient partial batch ingestion (never dropping good events because of one bad record), deduplication by `event_id`.
2. **Dashboard Query API (`GET /campaigns/{campaign_id}/stats`):** Thread-safe aggregation, accurate unique metrics, delivery rates, open rates, CTR, and UTC daily breakdowns.
3. **Concurreny & Correctness:** Robust synchronization (`sync.RWMutex`), zero data races under `-race`, end-to-end testing with real-world seed data.
4. **Paginated Event Listing (`GET /campaigns/{campaign_id}/events`):** Activity log for marketer troubleshooting with filtering and pagination.

---

## Part 4 — Scale Memo (100 Million Events / Day)

### 1. What Breaks First in the Current Implementation?
- **Memory Exhaustion (OOM):** Storing 100M events/day (approx. 15–20 GB/day uncompressed) in an in-memory map on a single instance will quickly exhaust RAM within days.
- **Lock Contention:** A single process with a global mutex or campaign-level locks processing ~1,200 events/sec sustained (with spikes exceeding 10,000 req/sec) will experience severe lock contention, increasing HTTP latency and causing provider timeouts.
- **Provider Timeouts & Cascading Retries:** If a slow aggregation or memory GC pause delays HTTP responses beyond provider timeout thresholds (e.g., 2–5s), providers will retry aggressively, creating a catastrophic retry storm.

---

### 2. What Would You Change, and in What Order?
1. **Decouple Ingestion from Processing (Asynchronous Queueing):**
   - Ingestion nodes become thin, stateless HTTP proxies that validate payloads, write raw messages to a durable log (Apache Kafka, Redpanda, or AWS SQS/Kinesis), and immediately return `202 Accepted`.
2. **Distributed Durable Storage:**
   - Migrate from in-memory maps to an analytical/time-series columnar database (ClickHouse or PostgreSQL with TimescaleDB) optimized for fast aggregations over billions of rows.
3. **Stream Processing for Real-Time Rollups:**
   - Implement stream workers (e.g., Go worker groups or Apache Flink) that consume from Kafka partitions, maintain deduplication caches, and write pre-aggregated counters.
4. **Read-Cache Layer for Dashboard Queries:**
   - Place Redis or a memory cache in front of dashboard read queries to serve `GET /campaigns/{id}/stats` with sub-10ms response times without hitting the primary analytical datastore.

---

### 3. API Contract & Storage Design Changes
- **API Contract:**
  - Ingestion endpoint shifts explicitly to asynchronous acknowledgment (`202 Accepted` with a batch tracking ID / summary of received items).
  - Add request compression support (`Content-Encoding: gzip/zstd`) and batch size limits (e.g., max 1,000 events per request).
- **Storage Schema:**
  - **Raw Events Table (Columnar / ClickHouse):** Partitioned by `toYYYYMM(timestamp)` and indexed by `(campaign_id, type, timestamp)`.
  - **Daily Rollup Table:** Pre-aggregated table `(campaign_id, date, sent_count, delivered_count, opened_count, clicked_count)`.
  - **HyperLogLog / Roaring Bitmaps:** For ultra-fast unique open and unique click estimations across millions of contacts.

---

### 4. Introducing a Queue: What New Problems Does It Create?
- **Read-Your-Writes Delay (Eventual Consistency):** Marketers hitting the dashboard right after triggering a campaign will experience a few seconds of pipeline lag.
- **Poison Pill Messages:** Corrupt messages that crash worker consumers must be quarantined to a Dead Letter Queue (DLQ) with alert thresholds.
- **Partitioning & Ordering Complexities:** Partitioning by `campaign_id` ensures events for a campaign are processed sequentially per partition, but requires partition rebalancing during hot campaigns.

---

### 5. Where Can Duplicates Sneak In?
- **Producer Retries on Network Acks:** A webhook proxy writes to Kafka but disconnects before returning HTTP 200 to the provider. The provider resends the batch.
- **Queue Consumer Rebalances:** If a stream consumer crashes before committing its Kafka offset, the replacement consumer reprocesses messages from the last committed offset.
- **Distributed Ingestion Races:** Two identical events arriving at different API nodes simultaneously.

---

### 6. Keeping Counters Trustworthy
- **Distributed Idempotency Cache:** Fast Redis/Aerospike key-value lookup for `event_id` with a sliding TTL (e.g., 7 days) combined with a database primary key constraint on `(campaign_id, event_id)`.
- **Atomic Upserts / Increments:** Utilize idempotent SQL upserts (`INSERT ... ON CONFLICT DO NOTHING`) or deduplicated stream joins.
- **Periodic Reconciliation Jobs:** Scheduled hourly background batch jobs that recompute aggregates from raw immutable event logs and reconcile any discrepancies in counter tables.

---

### 7. What Would You Monitor?
- **Ingestion Golden Signals:** Webhook HTTP request rate, p95/p99 latency, 4xx vs 5xx error rates.
- **Consumer Lag:** Kafka lag per consumer group (the earliest indicator that ingestion is outpacing processing capacity).
- **Duplicate Rate & Rejection Rate:** Spikes in duplicate `event_id`s or rejected payload schemas.
- **Dead Letter Queue (DLQ) Depth:** Immediate alerting if poison pills or processing errors accumulate.

---

### 8. What Would You Deliberately NOT Solve Yet?
- **Complex Multi-Region Active-Active Writes:** Unless regulatory or extreme latency needs dictate it, single-region multi-AZ deployment is vastly simpler and eliminates cross-region consensus overhead.
- **Microservice Over-Segmentation:** Keep ingestion and stats query services as simple modular services rather than introducing Kubernetes microservice mesh complexity prematurely.

---

## Part 5 — The Angry Marketer Investigation

### 1. Plausible Explanations Before Assuming Code is Broken
1. **Bot / Spam Click & Open Filtering:** Modern email providers (e.g., Apple Mail Privacy Protection, corporate security scanners) trigger automated opens/clicks upon delivery. A security filtering pipeline may have run retroactively between 10:00 and 10:30, scrubbing 2,000 false bot opens from the campaign metrics.
2. **Contact Deduplication & Identity Merge:** If 2,000 opens initially counted under distinct temporary contact IDs were reconciled into existing contact records, unique opens would decrease while delivered counts remain unaffected.
3. **Time-Window / Filter Adjustment:** The dashboard UI may have had a time filter applied (e.g., "Opens in Last 2 Hours"), which rolled forward between 10:00 and 10:30, excluding early morning opens from the visible window.
4. **GDPR / Right-to-be-Forgotten Data Deletion:** A batch privacy deletion or contact suppression request processed at 10:15 removed event records for opted-out contacts.
5. **Distributed Ingestion Lag & Stream Reconciliation:** The 10:00 snapshot was rendered from an un-deduplicated read replica or temporary accumulator; when the consolidation job committed at 10:15, duplicate events were merged.

---

### 2. Is the Delivered Number Rising Suspicious?
**No, it is completely normal and expected.**
- Out of 1,000,000 sent messages, delivery providers queue and throttle transmissions according to recipient ISP rate limits (e.g., Gmail, Yahoo).
- Deliveries take minutes or hours to complete. Receiving 5,000 additional delivery confirmations between 10:00 and 10:30 simply indicates that providers completed delivery for queued messages.

---

### 3. Step-by-Step Diagnostic Workflow
1. **Check Audit Logs & Pipeline Releases:** Verify if any bot-filtering algorithms, contact suppression runs, or code deployments occurred between 09:55 and 10:35.
2. **Query Raw Event Store:** Run an exact SQL count for the campaign:
   ```sql
   SELECT count(DISTINCT event_id), count(DISTINCT contact_id) 
   FROM events 
   WHERE campaign_id = 'cmp_xyz' AND type = 'opened';
   ```
3. **Inspect Deduplication & Update Logs:** Check whether `event_id` or `contact_id` deduplication was retroactively applied.
4. **Deciding if Bug vs. Expected:**
   - If an automated bot scrubber or deduplication pass intentionally updated the metric: **Expected behavior** (and an opportunity to improve dashboard UX by displaying "Bot-filtered opens: 2,000").
   - If raw immutable event counts decreased due to integer underflow, cache eviction, or race conditions in memory: **Bug** requiring immediate hotfix and historical replay.

---

## What I Completed / What I Intentionally Skipped

### Completed:
- Resilient Go backend service supporting streaming batch ingestion with graceful per-item error handling.
- Comprehensive campaign analytics endpoint providing both volume counts and rate KPIs (Delivery Rate, Open Rate, CTR, CTOR, Unique Opens, Daily UTC Buckets).
- Paginated event inspection endpoint (`GET /campaigns/{campaign_id}/events`) with type filtering.
- Thread-safe storage with idempotency and race-free concurrency guarantees.
- Fixed all 4 concurrency, deduplication, scope, and timezone bugs in the `debugging/` service.
- Full test suite with automated race condition checks and verification against real-world seed data.
- Built-in, zero-dependency interactive live marketing analytics web dashboard (`http://localhost:8080/`) with real-time KPI cards, rate gauges, daily UTC breakdown, and a webhook simulation tool.

### Intentionally Skipped:
- **Heavy Frontend Framework Overhead:** Kept the frontend completely dependency-free (pure Vanilla HTML5/CSS3/JS embedded via Go `embed.FS`) without requiring node/npm packages or external build chains.
- **External database dependencies:** Chose a clean, zero-dependency thread-safe in-memory architecture for instant portability and setup simplicity (`go run .`).


---

## If I Had Another Day
- **Persistent Embedded Storage:** Add pluggable SQLite/DuckDB backing with WAL mode enabled for durable local persistence across restarts.
- **Export & Streaming Webhooks:** Provide downstream webhook notifications when campaign milestones are reached (e.g., 90% delivery rate).
- **Interactive OpenMetrics / Prometheus Exporter:** Expose `/metrics` endpoint for real-time visualization of ingestion latency, batch sizes, duplicate rates, and rejected schemas.
- **Dynamic Bot-Click Heuristics:** Implement time-to-click heuristics (e.g., clicks occurring within <200ms of delivery classified as automated scanners).
