# Vulcan – Phase 3 Documentation

## Worker Scheduling & Resource Allocation

---

# Objective

Phase 3 introduces the scheduling layer of Vulcan.

Until Phase 2, the Control Plane was capable of creating tests and maintaining worker state through registration, heartbeats, reconciliation, and lifecycle management.

However, there was no mechanism for deciding **which workers should execute a test**.

The goal of this phase was to build a production-inspired scheduler capable of safely allocating workers under concurrent requests while maintaining consistency.

The implementation intentionally resembles techniques used in Kubernetes, Nomad and other orchestration platforms while remaining simple enough for a single developer to understand completely.

---

# What Was Built

## Scheduler Layer

A dedicated scheduler package was introduced between the Service layer and the Repository layer.

```
Test Service
      │
      ▼
 Scheduler
      │
      ▼
Worker Repository
```

The scheduler is responsible only for worker allocation.

It does **not**:

- communicate with workers
- execute tests
- monitor heartbeats
- generate metrics
- perform reconciliation

Keeping scheduling isolated makes it easier to replace scheduling algorithms in the future without affecting business logic.

---

## Worker Reservation

When a test is started, the scheduler attempts to reserve the requested number of workers.

Reservation consists of three operations performed atomically:

1. Lock idle workers.
2. Change their status to `RESERVED`.
3. Create entries in the `test_workers` table.

If any step fails, the transaction is rolled back.

---

# Scheduling Algorithm

Current scheduling algorithm:

1. Begin PostgreSQL transaction.
2. Find idle workers.
3. Lock selected rows.
4. Skip already locked rows.
5. Reserve workers.
6. Create worker-test mappings.
7. Commit transaction.

Worker selection query:

```sql
SELECT id
FROM workers
WHERE status = 'IDLE'
ORDER BY last_heartbeat DESC
FOR UPDATE SKIP LOCKED
LIMIT $1;
```

Workers with the most recent heartbeat are preferred.

This provides a simple heuristic for selecting healthy workers.

---

# Why FOR UPDATE SKIP LOCKED?

Without row-level locking:

```
Request A
       \
        Worker 5
       /
Request B
```

Both requests could reserve the same worker.

Using:

```sql
FOR UPDATE SKIP LOCKED
```

The first transaction locks selected rows.

The second transaction automatically skips those rows and selects different workers.

This guarantees that two tests can never reserve the same worker simultaneously.

---

# PostgreSQL Transaction

Worker reservation is performed inside a single database transaction.

```
BEGIN

↓

Select workers

↓

Lock rows

↓

Update status

↓

Insert mappings

↓

COMMIT
```

If any operation fails:

```
ROLLBACK
```

This guarantees consistency between worker state and assignment records.

---

# Test-Worker Mapping

A new table was introduced:

```sql
test_workers
```

Schema:

```text
test_id
worker_id
assigned_at
```

Purpose:

- records worker ownership
- supports releasing workers
- enables future execution tracking
- provides many-to-many mapping between tests and workers

Primary key:

```
(test_id, worker_id)
```

Foreign keys:

- tests(id)
- workers(id)

---

# Worker Lifecycle

Scheduling introduced a new lifecycle.

```
IDLE

↓

RESERVED

↓

IDLE
```

Future phases extend this to:

```
IDLE

↓

RESERVED

↓

RUNNING

↓

DRAINING

↓

IDLE
```

Only `IDLE` workers are eligible for scheduling.

---

# Start Test Flow

API

```
POST /api/v1/tests/{id}/start
```

Example request:

```json
{
    "worker_count": 2
}
```

Flow:

```
Client

↓

Handler

↓

Service

↓

Scheduler

↓

Repository

↓

PostgreSQL Transaction

↓

Reserve Workers

↓

Create test_workers

↓

Update Test Status

↓

Return Reserved Workers
```

Successful response:

```json
{
    "test_id": "...",
    "status": "RUNNING",
    "workers": [
        {
            "id": "...",
            "hostname": "worker-1"
        },
        {
            "id": "...",
            "hostname": "worker-2"
        }
    ]
}
```

---

# Stop Test Flow

Stopping a test now performs worker release before changing test status.

Flow:

```
Stop Test

↓

Find Assigned Workers

↓

Update Workers

RESERVED

↓

IDLE

↓

Delete test_workers mappings

↓

Update Test Status

↓

STOPPED
```

Workers immediately become available for future scheduling.

---

# Repository Changes

## WorkerRepository

Added:

```go
ReserveWorkersForTest(
    ctx,
    testID,
    workerCount,
)
```

```go
ReleaseWorkersForTest(
    ctx,
    testID,
)
```

---

## TestRepository

Extended to persist:

- worker_count
- status transitions

Existing methods reused:

- CreateTest
- GetTestByID
- UpdateStatus

No duplicate repository methods were introduced.

---

# Database Changes

## tests

Added column:

```text
worker_count
```

Stores the requested number of workers for the test.

---

## test_workers

New table:

```sql
CREATE TABLE test_workers (
    test_id TEXT,
    worker_id TEXT,
    assigned_at TIMESTAMPTZ,

    PRIMARY KEY(test_id, worker_id)
);
```

Index:

```sql
CREATE INDEX idx_test_workers_worker_id
ON test_workers(worker_id);
```

---

# Design Decisions

## Raw SQL

Scheduling relies entirely on handwritten SQL.

Reasons:

- explicit transactions
- explicit locking
- complete understanding
- no ORM abstraction

---

## No Generic Scheduler

Only one scheduling strategy exists.

Adding abstractions for multiple scheduling algorithms would increase complexity without providing any benefit.

Alternative strategies may include:

- Least-loaded
- CPU-aware
- Zone-aware
- Rack-aware

These are intentionally deferred.

---

## Scheduler Is Separate From Service

Business logic should not understand row locking or transaction details.

Responsibilities are divided as follows.

Service:

- validates workflow
- updates test status

Scheduler:

- allocates workers

Repository:

- performs SQL operations

---

# Failure Scenarios

## Concurrent Starts

Solved using:

```sql
FOR UPDATE SKIP LOCKED
```

Duplicate worker allocation cannot occur.

---

## Insufficient Workers

If fewer workers are available than requested:

- reservation fails
- transaction rolls back
- no mappings created
- no workers reserved

HTTP response:

```
409 Conflict
```

---

## Transaction Failure

Any database failure causes rollback.

Workers remain IDLE.

Mappings are not created.

---

## Worker Dies During Reservation

Worker liveness is handled independently by the reconciler.

Reservation and heartbeat are intentionally decoupled.

---

## Worker Stops Sending Heartbeats

Current behaviour:

```
RESERVED

↓

OFFLINE
```

This is expected.

Scheduling never fakes heartbeats.

Future phases introduce automatic recovery.

---

# APIs

## Existing

```
POST /api/v1/tests

GET /api/v1/tests

GET /api/v1/tests/{id}

POST /api/v1/tests/{id}/stop
```

## New

```
POST /api/v1/tests/{id}/start
```

---

# Testing Performed

Verified:

- Worker registration
- Test creation
- Worker reservation
- Status transition to RESERVED
- Creation of test_workers mappings
- Test status changes to RUNNING
- Worker release
- Mapping deletion
- Worker status returns to IDLE
- Test status changes to STOPPED

Concurrent allocation behaviour verified through PostgreSQL row locking.

---

# Current Project State

Completed:

- Phase 0 – Architecture
- Phase 1 – Control Plane Foundation
- Phase 2 – Worker Management
- Phase 3 – Worker Scheduling

Current worker lifecycle:

```
IDLE

↓

RESERVED

↓

IDLE
```

The scheduler is now capable of safely allocating workers using PostgreSQL row-level locking while maintaining transactional consistency.

Execution of tests is intentionally deferred to Phase 4.