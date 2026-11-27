# Batch Book Recovery Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans in this session. User requires direct work on v88; do not create a branch or worktree.

**Goal:** Restore truthful queue status and the explicitly approved book's existing bodies on local 4001.

**Architecture:** Reconcile API job projections with the configured queue without rewriting historical runs or enqueueing old jobs. Recover the approved book in a guarded transaction from existing, hash-verified bodies; no model or submission calls.

**Tech Stack:** Go, React, MySQL, Redis, Go embed; Python for guarded local data recovery and exported evidence.

## Global Constraints

- Preserve unrelated dirty files, remain on v88, no push or public release.
- No new paid model requests, retries, or external submissions.
- Restore only owner choushiyiguai / book 7705784637234105369.
- Never invent original task dates, classification, model provenance or submission receipts.
- Retain full original_raw; local first; frontend source -> React build -> Go embed.

### Task 1: Queue status projections

Files: create `backend/internal/httpapi/novel_fetch_job_projection.go` and its test; modify `novel_fetch_workshop.go`, `novel_fetch_automation.go`.

Interface: `verifiedWorkshopJobView(ctx context.Context, store novelfetchworkshop.Store, queue novelfetchworkshop.ProcessQueue, owner string, view map[string]any) map[string]any`.

- [x] Add regression cases for queue_, go-workshop-, and go-schedule- historical queued/running jobs absent from the queue; require failed with queue_missing reason, immutable history and no enqueue.
- [x] Verify existing jobs stay active, Redis errors remain unverified rather than missing, completed runs remain terminal, queue-less callers retain behavior.
- [x] Run failing focused Go tests before implementing the helper.
- [x] Apply projection to latest, individual job, and realtime/automation endpoints. Preserve existing manual stop projection.
- [x] Run focused queue, schedule, workflow and HTTP regression tests.

### Task 2: Frontend state

Files: `frontend/src/user/pages/novel-fetch-progress.js`, its test, `NovelFetchPage.jsx`.

- [x] Add behavioral tests that interrupted/error projections and unavailable queue checks never render historical waiting/progress as current work.
- [x] Clear processJob when latest is empty; retain transient polling failures.
- [x] Give current and history empty tables accurate wording; avoid inventing historical batches.
- [x] Run Node focused tests and React build.

### Task 3: Guarded single-book recovery

Files: task-owned archive recovery script and evidence manifest.

- [x] Verify exported SHA256 matches each existing DB body. Save before snapshot.
- [x] Open transaction, lock document identity and body rows, require all expected versions and hashes, reject non-recovery existing document.
- [x] Insert minimal recovery document with real recovery time and explicit unknown original task date; preserve existing body revisions/content and reset only release/expiry lifecycle.
- [x] Keep automatic generation/submission disabled for recovered task; preserve all old runs and unrelated bodies.
- [x] Commit transaction, verify hashes and counts; idempotent rerun must not mutate the recovered record.

### Task 4: Local delivery

- [x] Review and commit only this task's source/docs/test changes to v88.
- [ ] Build frontend and Go embedded binary from current source with traceable manifest. Confirm current listener and no active generation before restart; preserve runtime environment.
- [ ] Verify /health and /api/runtime-build-info, then logged-in browser task list, body versions and queue logs.
- [ ] Record actual runtime SHA, test evidence, restored body hashes and limitations; no external upload or generation.

Current checkpoint: frontend and Windows embedded Go candidate built; data and browser body acceptance passed; actual 4001 still c33aa9d3 because another 25-book comic batch has 24 running books and one queued. Do not restart it without a safe idle window or user direction. Details: docs/acceptance/2026-11-27-batch-book-recovery.md.
