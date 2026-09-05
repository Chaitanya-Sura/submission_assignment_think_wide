# AI Usage Disclosure

## 1. Tools Used
- **Antigravity AI Assistant (powered by Google Gemini 3.7 Flash)**: Used for architecture review, code generation, concurrency analysis, test design, and drafting analytical memos.

---

## 2. What the Tools Were Used For
- **Scaffolding and Boilerplate:** Generating HTTP handler routing, JSON response structures, and unit/integration test suites in Go.
- **Concurrency & Race Condition Analysis:** Inspecting goroutine interactions in `debugging/main.go` and verifying data race behavior under Go's race detector.
- **Analytical Memo Structuring:** Organizing the scale memo (Part 4) and operational triage workflow (Part 5) into structured, clear sections covering storage tradeoffs, stream ingestion, and telemetry.

---

## 3. One Suggestion From AI That Was Rejected or Changed, and Why
- **Initial Suggestion:** In `starter/main.go`, the initial boilerplate suggestion used `json.NewDecoder(r.Body).Decode(&events)` directly into a slice of struct `[]Event`.
- **Why Rejected / Changed:** Direct unmarshaling of the entire request body into `[]Event` causes the entire HTTP batch to abort with an error if even one event contains an invalid field (e.g., `"timestamp": "not-a-time"` or an unknown event type). This violates resilient webhook ingestion principles, where 99 good events should never be discarded due to 1 corrupted item. We rejected this in favor of a two-pass parser using `[]json.RawMessage` and per-event validation in `store.IngestBatch()`, returning an informative `IngestResult` with `accepted`, `duplicates`, `rejected`, and field error breakdowns.

---

## 4. One Thing AI Helped Me Understand
- **Composite Keying for Multi-Campaign Unique Counters:** When debugging `openedBy` in Part 3, AI highlighted how a global set keyed purely by `contact_id` silently creates cross-campaign coupling. An interaction in campaign A would permanently suppress the unique open count for that same contact in campaign B. Using a composite key `campaign_id + ":" + contact_id` cleanly decoupled campaign metrics while preserving O(1) in-memory lookups.

---

## 5. Anything AI Generated That Had to Be Debugged
- **Goroutine Synchronization in apply():** In Part 3, an early AI suggestion attempted to remove concurrency entirely by processing events synchronously in `processBatch`. While this eliminated data races, it removed the worker pool structure that the original program intended to test. We adjusted the solution to keep the 8 worker goroutines and channel fan-out intact while adding a targeted mutex lock in `apply()`, adhering to the assignment's rule: *"Make minimal fixes. Do not rewrite or restructure the program."*
