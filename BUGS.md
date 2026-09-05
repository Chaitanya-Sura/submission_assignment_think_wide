# Part 3 — Debugging Findings & Fixes

This document records the four bugs identified, analyzed, fixed, and verified in `debugging/main.go`.

---

## Bug 1: Batch-Local Deduplication (`seen` map recreation)

### 1. What the bug is
Duplicate events across different batches were not deduplicated. If the provider retried an event and the duplicate appeared in a later batch, it was processed and counted multiple times, violating Rule 1: *"Count each `event_id` at most once across the whole file."*

### 2. Why it happens
In `processBatch(events []Event)`, the deduplication map `seen` was initialized as a local variable inside the function (`seen := make(map[string]bool)`). Consequently, every batch started with an empty `seen` map and lost all knowledge of event IDs encountered in prior batches.

### 3. Your fix
Lifted `seen` out of `processBatch` to package-level scope (`var seen = map[string]bool{}`) alongside other state structures (`stats`, `openedBy`), ensuring global deduplication across all batches processed by the file scanner.

### 4. How verified
Verified by comparing output against `expected_output.txt`. Prior to the fix, `sent`, `delivered`, `opened`, and `clicked` totals were higher than expected due to un-deduplicated provider retries in later batches. With global tracking, counts strictly align with unique `event_id` occurrences.

---

## Bug 2: Global vs. Per-Campaign Unique Open Tracking (`openedBy` keying)

### 1. What the bug is
`unique_opens` for campaigns were undercounted when a recipient contact opened messages across more than one campaign. Rule 2 specifies: *"The same contact opening in two campaigns counts once in each."*

### 2. Why it happens
`openedBy` was a flat global map keyed solely by `ev.ContactID` (`map[string]bool`). When a contact opened an email in campaign A, `openedBy[contactID]` became `true`. If the same contact later opened an email in campaign B, `track()` evaluated `!openedBy[ev.ContactID]` as `false`, skipping the `cs.UniqueOpens++` increment for campaign B.

### 3. Your fix
Changed the lookup key in `track()` from `ev.ContactID` to a composite key combining campaign and contact:
```go
key := ev.CampaignID + ":" + ev.ContactID
if !openedBy[key] {
    openedBy[key] = true
    cs.UniqueOpens++
}
```

### 4. How verified
Cross-verified against campaigns in `expected_output.txt` where multiple campaigns targeted overlapping contacts. `unique_opens` matched the expected values for all campaigns (`cmp_A` through `cmp_E`).

---

## Bug 3: Machine-Local Timezone in Daily Delivered Aggregation

### 1. What the bug is
Daily delivery buckets produced incorrect dates and counts depending on the local time zone of the host machine running the code, violating Rule 3: *"Daily delivered buckets use the event's UTC date. The timestamp field is UTC."*

### 2. Why it happens
In `track()`, the daily date key was formatted using `.Local()`:
```go
day := ev.Timestamp.Local().Format("2006-01-02")
```
For any system whose local timezone is not UTC (e.g., UTC+05:30 or UTC-07:00), timestamps near UTC midnight shifted into the previous or following calendar day, altering daily bucket counts.

### 3. Your fix
Replaced `.Local()` with `.UTC()`:
```go
day := ev.Timestamp.UTC().Format("2006-01-02")
```

### 4. How verified
Verified that daily buckets strictly group by UTC dates `2026-08-01` through `2026-08-07` matching `expected_output.txt` regardless of host timezone settings.

---

## Bug 4: Concurrent Map / Counter Mutation Race Condition

### 1. What the bug is
Worker goroutines concurrently updated `CampaignStats` counters (`cs.Sent++`, `cs.Delivered++`, etc.) without synchronization, leading to data races and lost updates under concurrency.

### 2. Why it happens
`processBatch` dispatches events across 8 concurrent worker goroutines reading from `jobs` channel and calling `apply(ev)`. The counter increments (`cs.Sent++`, etc.) are non-atomic read-modify-write operations on shared pointer `cs := stats[ev.CampaignID]`. Multiple workers processing events for the same campaign simultaneously cause data races (flagged by `go run -race`).

### 3. Your fix
Introduced a mutex `var mu sync.Mutex` to synchronize `apply(ev)`:
```go
var mu sync.Mutex

func apply(ev Event) {
    mu.Lock()
    defer mu.Unlock()
    cs := stats[ev.CampaignID]
    switch ev.Type {
    case "sent":
        cs.Sent++
    case "delivered":
        cs.Delivered++
    case "opened":
        cs.Opened++
    case "clicked":
        cs.Clicked++
    }
}
```

### 4. How verified
Verified with `go run -race . events.jsonl`, confirming 0 data races detected, completely clean execution, and deterministic output identical to `expected_output.txt` on every run.
