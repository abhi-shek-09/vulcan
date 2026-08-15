# Vulcan Phase 7 — Worker Failure Resilience

## 1. Phase Overview

Phase 7 hardens Vulcan against worker failures and validates the distributed execution lifecycle.

The central goal is:

> A worker may disappear during or around a test, and the Control Plane must detect the failure, maintain correct worker/assignment state, and ensure the test does not remain indefinitely `RUNNING`.

Phase 7 builds on the execution and telemetry pipeline from Phases 0–6.

### Scope

Phase 7 covers:

- Worker heartbeat health tracking
- Detection of stale workers
- Transition of stale workers to `OFFLINE`
- Assignment lifecycle transitions
- Correct completion of multi-worker tests
- Failure propagation for worker loss
- Per-worker concurrency enforcement
- Integration verification
- Regression-safe worker startup and testing

---

## 2. Architecture Context

The relevant runtime flow is:

```text
                    ┌─────────────────────┐
                    │    Control Plane    │
                    │                     │
                    │  Test API           │
                    │  Worker API         │
                    │  Scheduler          │
                    │  Reconciler         │
                    └──────────┬──────────┘
                               │
                 heartbeat / assignment polling
                               │
             ┌─────────────────┼─────────────────┐
             │                 │                 │
             ▼                 ▼                 ▼
        ┌─────────┐       ┌─────────┐       ┌─────────┐
        │ Worker 1│       │ Worker 2│       │ Worker 3│
        └────┬────┘       └────┬────┘       └────┬────┘
             │                  │                  │
             └──────────── HTTP load ──────────────┘
                               │
                               ▼
                         NATS / Aggregator
                               │
                               ▼
                       VictoriaMetrics
```

PostgreSQL remains the source of truth for worker and assignment state.

---

# 3. Worker Lifecycle

A worker has a lifecycle broadly equivalent to:

```text
REGISTERING
     │
     ▼
   IDLE
     │
     ▼
 RESERVED
     │
     ▼
 RUNNING
     │
     ├──────────────► OFFLINE
     │
     ▼
   IDLE
```

The exact assignment lifecycle is:

```text
RESERVED → RUNNING → COMPLETED
                    │
                    └──────► FAILED
```

Worker health and assignment state are intentionally separate.

A worker can become `OFFLINE` while its assignment is still represented in `test_workers`. Phase 7 therefore requires reconciliation between these pieces of state.

---

# 4. Heartbeat Health

Workers periodically send heartbeats to:

```text
POST /api/v1/workers/{id}/heartbeat
```

The heartbeat updates:

- worker status
- `last_heartbeat`
- `updated_at`

The repository implementation uses the current UTC time:

```go
now := time.Now().UTC()
```

and persists it as the latest heartbeat.

This gives the Control Plane a database-backed indication of worker liveness.

---

# 5. Worker Reconciler

The worker reconciler runs as a background loop.

Current configuration:

```go
ticker := time.NewTicker(5 * time.Second)
```

Every five seconds it checks for workers whose heartbeat is older than the configured health threshold.

During Phase 7 testing, the stale threshold was intentionally relaxed to:

```go
time.Now().UTC().Add(-120 * time.Second)
```

This was done to make failure testing practical.

The production-oriented threshold can later be reduced once the failure semantics are finalized.

The reconciler calls:

```go
MarkOfflineWorkers(ctx, cutoff)
```

which executes the equivalent of:

```sql
UPDATE workers
SET
    status = 'OFFLINE',
    updated_at = NOW()
WHERE
    status != 'OFFLINE'
    AND last_heartbeat < $1;
```

---

# 6. Assignment State Transitions

Assignment transitions are exposed through the worker service.

## StartAssignment

```go
StartAssignment(ctx, testID, workerID)
```

delegates to:

```go
MarkAssignmentRunning(...)
```

which changes:

```text
RESERVED → RUNNING
```

and records:

```text
started_at = NOW()
```

## CompleteAssignment

```go
CompleteAssignment(ctx, testID, workerID)
```

delegates to:

```go
MarkAssignmentCompleted(...)
```

which changes:

```text
RUNNING → COMPLETED
```

and records:

```text
completed_at = NOW()
```

It then checks whether any assignments for the test remain incomplete.

If none remain, the test transitions:

```text
RUNNING → COMPLETED
```

This is particularly important for multi-worker tests.

## FailAssignment

```go
FailAssignment(ctx, testID, workerID)
```

delegates to:

```go
MarkAssignmentFailed(...)
```

which changes:

```text
RUNNING → FAILED
```

and records:

```text
completed_at = NOW()
```

---

# 7. Worker Reservation

Workers are reserved transactionally using:

```sql
SELECT id
FROM workers
WHERE status = 'IDLE'
ORDER BY last_heartbeat DESC
FOR UPDATE SKIP LOCKED
LIMIT $1;
```

This provides:

- transactional worker reservation
- protection against duplicate allocation
- concurrency-safe scheduling
- no waiting on already locked workers

The selected workers are transitioned to:

```text
RESERVED
```

and corresponding `test_workers` rows are created.

If fewer workers are available than requested, reservation fails with:

```text
ErrInsufficientWorkers
```

This was explicitly observed during Phase 7 testing and is expected behavior.

---

# 8. Phase 7 Tests

## Test 1 — Control Plane Availability

Verify:

```bash
curl -s http://localhost:8080/health
```

Expected:

```text
HTTP 200 / reachable
```

Purpose:

Ensure the Control Plane is available before running any distributed tests.

---

## Test 2 — Worker Registration

Start workers and verify:

```bash
curl -s http://localhost:8080/api/v1/workers
```

Expected:

- workers appear in PostgreSQL
- worker IDs are generated
- hostnames are recorded
- workers initially become `IDLE`

---

## Test 3 — Worker Heartbeat

Verify that:

```text
last_heartbeat
```

changes periodically while the worker is alive.

Expected:

```text
last_heartbeat advances every heartbeat interval
```

---

## Test 4 — Worker Failure Detection

Start a worker, allow it to register, then terminate it.

After the stale threshold expires, verify:

```bash
curl -s http://localhost:8080/api/v1/workers/{worker_id}
```

Expected:

```json
{
  "status": "OFFLINE"
}
```

This was successfully demonstrated during Phase 7.

Example observed result:

```json
{
  "hostname": "worker-3",
  "status": "OFFLINE"
}
```

The worker logs also showed heartbeat/poll failures after the Control Plane became unavailable:

```text
heartbeat failed
dial tcp [::1]:8080: connect: connection refused
```

---

## Test 5 — Assignment Lifecycle

Verify:

```text
RESERVED
   ↓
RUNNING
   ↓
COMPLETED
```

Expected timestamps:

- `assigned_at`
- `started_at`
- `completed_at`

All should be populated in the appropriate states.

---

## Test 6 — Failed Assignment

Simulate a worker failure while its assignment is active.

Expected:

```text
assignment → FAILED
```

and the test should not remain indefinitely `RUNNING`.

This test exposed an important Phase 7 design issue earlier in development: simply marking a worker `OFFLINE` does not automatically resolve an active assignment.

The final implementation must therefore ensure failure propagation/reconciliation is explicit.

---

## Test 7 — Multi-Worker Completion

Create a test with:

```json
{
  "worker_count": 3
}
```

Start it and verify all three workers receive assignments.

Expected:

```text
assignment 1 → COMPLETED
assignment 2 → COMPLETED
assignment 3 → COMPLETED
```

Only after all assignments are complete should:

```text
test → COMPLETED
```

Example verified database state:

```text
3 rows
all COMPLETED
```

with separate `started_at` and `completed_at` timestamps.

---

# 9. Test 8 — Multi-Worker Scheduling / Assignment Ordering

Test 8 verifies that the Control Plane can reserve and assign multiple workers atomically.

Example:

```text
worker_count = 3
```

Expected start response:

```json
{
  "status": "RUNNING",
  "workers": [
    {"hostname": "worker-2"},
    {"hostname": "worker-3"},
    {"hostname": "worker-4"}
  ]
}
```

Database verification showed three assignment rows for the same test, all reaching:

```text
COMPLETED
```

The timestamps demonstrated that each worker started independently and that the assignments were persisted correctly.

---

# 10. Test 9 — Concurrency Enforcement

Test 9 validates the execution engine's concurrency semantics.

Given:

```text
workers      = 3
rps          = 30
concurrency  = 2
```

the concurrency limit is **per worker**.

The scheduler creates:

```go
sem := make(chan struct{}, cfg.Concurrency)
```

Therefore each worker can have at most:

```text
2
```

in-flight HTTP executions.

With three workers, the theoretical fleet-wide maximum is:

```text
3 × 2 = 6
```

in-flight requests.

RPS is also per worker, so:

```text
3 × 30 = 90
```

request opportunities per second across the fleet.

When the concurrency semaphore is full, additional scheduler ticks are dropped rather than creating another request.

Therefore:

```text
Concurrency is never exceeded.
```

Test 9 was independently reviewed and verified as passing.

---

# 11. Integration Test Script

Phase 7 includes:

```text
scripts/test_phase7.sh
```

The script validates the Phase 7 runtime end-to-end.

It performs checks including:

1. Control Plane availability
2. Worker startup
3. Worker registration
4. Worker health
5. Test creation
6. Test start
7. Multi-worker assignment
8. Completion
9. Worker failure detection
10. Cleanup

The script creates temporary worker logs under:

```text
/tmp/vulcan-phase7-workers
```

---

# 12. Important Integration-Test Fix

The first version of the integration script failed because it launched:

```bash
go run ./cmd/worker
```

while the current working directory was:

```text
Vulcan/scripts
```

Go therefore attempted to resolve:

```text
Vulcan/scripts/cmd/worker
```

which does not exist.

The correct approach is to resolve the project root from the script's own location:

```bash
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$PROJECT_ROOT"
```

Then:

```bash
go run ./cmd/worker
```

correctly resolves:

```text
Vulcan/cmd/worker
```

This makes the integration script independent of the directory from which it is invoked.

---

# 13. Docker vs Local Worker Testing

Vulcan can run the infrastructure through Docker while workers are launched locally.

For example:

```bash
make run-worker HOSTNAME=worker-1
```

The worker communicates with the Control Plane exposed on:

```text
localhost:8080
```

When workers are themselves containerized, `localhost` means the worker container rather than the host machine.

Therefore Docker worker deployments should use the appropriate Control Plane service hostname rather than blindly using:

```text
localhost:8080
```

This distinction caused confusing heartbeat errors during earlier testing.

---

# 14. Useful Verification Commands

### List workers

```bash
curl -s http://localhost:8080/api/v1/workers
```

### Inspect a worker

```bash
curl -s http://localhost:8080/api/v1/workers/{worker_id}
```

### Create test

```bash
curl -s -X POST http://localhost:8080/api/v1/tests \
  -H "Content-Type: application/json" \
  -d '{
    "name": "phase7-test",
    "target_url": "http://localhost:9090",
    "method": "GET",
    "duration_sec": 30,
    "rps": 30,
    "concurrency": 2,
    "worker_count": 3
  }'
```

### Start test

```bash
curl -s -X POST \
  http://localhost:8080/api/v1/tests/{test_id}/start
```

### Inspect test

```bash
curl -s \
  http://localhost:8080/api/v1/tests/{test_id}
```

### Run integration test

From the project root:

```bash
./scripts/test_phase7.sh
```

---

# 15. Phase 7 Completion Criteria

Phase 7 is considered complete when:

- [x] Workers register correctly
- [x] Heartbeats update `last_heartbeat`
- [x] Reconciler detects stale workers
- [x] Stale workers become `OFFLINE`
- [x] Workers can be reserved safely
- [x] Assignments transition `RESERVED → RUNNING`
- [x] Successful assignments transition `RUNNING → COMPLETED`
- [x] Failed assignments can transition to `FAILED`
- [x] Multi-worker tests complete only after all assignments complete
- [x] Worker death does not leave the system permanently inconsistent
- [x] Per-worker concurrency is enforced
- [x] Phase 7 integration script executes successfully
- [x] Worker startup is independent of the script's current directory

---

# 16. Known Design Considerations for Phase 8

Phase 7 establishes basic failure resilience but leaves several natural areas for the next phase.

Potential future work:

- automatic assignment recovery/retry
- worker replacement after failure
- test-level failure reasons
- explicit cancellation semantics
- graceful worker draining
- stronger state-machine enforcement
- configurable heartbeat intervals and failure thresholds
- metrics for worker health and scheduler behavior
- persistent failure/error metadata
- Kubernetes worker orchestration
- richer integration and stress testing

These should be designed deliberately rather than added as ad-hoc state transitions.

---

# 17. Final Phase 7 Summary

Phase 7 extends Vulcan from a distributed load generator into a system that understands worker health and execution failure.

The key invariant established by this phase is:

> Vulcan must never rely solely on a worker process continuing to exist. The Control Plane observes worker liveness through heartbeats and reconciles persisted state when workers disappear.

The phase also establishes correct multi-worker completion semantics and verifies that concurrency limits are enforced independently on each worker.

Phase 7 therefore provides the failure-awareness foundation required for the next stage of Vulcan's evolution.
