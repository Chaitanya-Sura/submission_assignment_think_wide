# AI Usage Disclosure

## 1. Tools & Models Used
- **Antigravity IDE & AI Assistant Ecosystem**: Utilized various underlying foundation models available within Antigravity across different phases of the project, including:
  - **Google Gemini Models (Gemini 3.7 Flash, Gemini 3.8 Pro)**: Primary models for rapid codebase exploration, real-time code generation, high-level architectural brainstorming, and drafting analytical memos.
  - **Anthropic Claude Models (Claude 3.5 / 3.7 Sonnet via Antigravity)** & **OpenAI GPT Models**: Used for deep code reasoning, concurrency validation, and reviewing complex Go logic.
- **Collaborative Human + AI Paradigm**: The entire project was developed through an integrated, mixed human-AI pair programming workflow across every folder (`starter/`, `debugging/`, `NOTES.md`, `BUGS.md`, `README.md`). AI acted as an accelerator for scaffolding, diagnostics, and drafting, while human oversight drove architectural decisions, domain modeling, verification against business constraints, prompt refinement, and final code edits.

---

## 2. What the Tools Were Used For & Mixed Human-AI Workflow
Across the entire repository:
- **`starter/` (Service Implementation & Design)**:
  - *AI Contribution:* Scaffolded HTTP handler boilerplate, routing patterns, struct definitions, and initial unit test templates.
  - *Human Contribution & Code Mixing:* Designed the resilient two-pass partial batch ingestion engine, added precise validation rules, implemented thread-safe storage abstractions (`sync.RWMutex`), tuned metric calculation algorithms (unique open/click tracking, CTR/CTOR, daily UTC groupings), and refined tests to ensure zero regressions.
- **`debugging/` & `BUGS.md` (Diagnostic & Bug Resolution)**:
  - *AI Contribution:* Assisted in static analysis of goroutine concurrency patterns and highlighting potential race conditions.
  - *Human Contribution & Code Mixing:* Identified root causes under `-race` flag, rejected invasive rewrites in favor of minimal localized surgical fixes (mutex in `apply()`, composite key `campaign_id:contact_id`, UTC normalization, and lifting deduplication scope), and verified outputs against `expected_output.txt`.
- **`NOTES.md` (Architecture & Analysis)**:
  - *AI Contribution:* Drafted initial outlines and structured bullet points for system scaling trade-offs and telemetry signals.
  - *Human Contribution & Code Mixing:* Refined all reasoning, customized the scale memo for 100M events/day (Kafka stream architecture, ClickHouse storage, idempotency caching), and authored deep diagnostic operational workflows for the marketer telemetry discrepancies.

---

## 3. One Suggestion From AI That Was Rejected or Changed, and Why
- **Initial Suggestion:** In `starter/main.go`, an initial AI boilerplate suggestion used `json.NewDecoder(r.Body).Decode(&events)` directly into a single slice of struct `[]Event`.
- **Why Rejected / Changed:** Direct unmarshaling of the entire request body into `[]Event` causes the entire HTTP batch to abort with an error if even one event contains an invalid field (e.g., `"timestamp": "not-a-time"` or an unknown event type). This violates resilient webhook ingestion principles, where 99 good events should never be discarded due to 1 corrupted item. We rejected this in favor of a resilient two-pass parser using `[]json.RawMessage` and per-event validation in `store.IngestBatch()`, returning an informative `IngestResult` with `accepted`, `duplicates`, `rejected`, and field error breakdowns.

---

## 4. One Thing AI Helped Me Understand
- **Composite Keying for Multi-Campaign Unique Counters:** When debugging `openedBy` in Part 3, AI highlighted how a global set keyed purely by `contact_id` silently creates cross-campaign coupling. An interaction in campaign A would permanently suppress the unique open count for that same contact in campaign B. Using a composite key `campaign_id + ":" + contact_id` cleanly decoupled campaign metrics while preserving O(1) in-memory lookups.

---

## 5. Anything AI Generated That Had to Be Debugged
- **Goroutine Synchronization in apply():** In Part 3, an early AI suggestion attempted to remove concurrency entirely by processing events synchronously in `processBatch`. While this eliminated data races, it removed the worker pool structure that the original program intended to test. We adjusted the solution to keep the 8 worker goroutines and channel fan-out intact while adding a targeted mutex lock in `apply()`, adhering to the assignment's rule: *"Make minimal fixes. Do not rewrite or restructure the program."*

