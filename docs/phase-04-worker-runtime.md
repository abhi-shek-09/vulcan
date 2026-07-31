# Vulcan Control Plane
# Phase 4 Documentation
## Distributed Scheduling & Worker Runtime

**Version:** 1.0

**Status:** Completed

---

# Table of Contents

1. Introduction
2. Phase 4 Goals
3. Architecture Overview
4. Evolution from Previous Phases
5. High-Level Scheduling Architecture
6. Component Responsibilities
7. Worker Lifecycle
8. Database Changes
9. Assignment Model
10. Scheduler Design
11. Worker Reservation Algorithm
12. Design Decisions

---

# 1. Introduction

Phase 4 transforms Vulcan from a simple Control Plane into a distributed scheduling system.

Until the end of Phase 3, workers could register themselves and periodically report their health through heartbeats. Tests could be created, listed and stopped, but no actual relationship existed between a test and a worker.

The Control Plane knew about workers.

The Control Plane knew about tests.

However, it had absolutely no knowledge of:

- which worker should execute which test
- how workers receive work
- how multiple workers can safely execute different tests simultaneously
- how worker failures should be handled during scheduling

Phase 4 introduces all of these capabilities.

For the first time, Vulcan behaves like a real distributed system.

---

# 2. Phase Objectives

The primary objective of Phase 4 is to introduce distributed scheduling.

Instead of treating workers as standalone registered machines, the Control Plane now manages them as schedulable compute resources.

The Control Plane becomes responsible for:

- selecting workers
- reserving workers
- assigning work
- tracking assignment progress
- recovering dead workers
- exposing assignments through APIs
- enabling autonomous worker execution

At the end of this phase a worker can:

1. Register itself.
2. Send heartbeats.
3. Poll for work.
4. Receive an assignment.
5. Mark it as RUNNING.
6. Execute the workload.
7. Mark it COMPLETE.
8. Continue polling for more work.

No manual intervention is required once the worker starts.

---

# 3. Architecture Overview

The architecture after Phase 4 looks as follows.

```
                     +----------------------+
                     |    Control Plane     |
                     +----------+-----------+
                                |
              +-----------------+-----------------+
              |                                   |
              |                                   |
      Scheduler Service                  Worker Repository
              |                                   |
              +-----------------+-----------------+
                                |
                           PostgreSQL
                                |
        +-----------------------+-----------------------+
        |                                               |
      workers                                      test_workers
        |                                               |
        +-----------------------+-----------------------+
                                |
                     Assignment API Endpoints
                                |
                      Poll / Heartbeat APIs
                                |
        ---------------------------------------------------
        |                     |                     |
     Worker 1             Worker 2             Worker 3
        |                     |                     |
 Register → Poll → Execute → Complete → Poll Again
```

Unlike previous phases, workers are no longer passive database entries.

They actively communicate with the Control Plane.

---

# 4. Evolution From Previous Phases

## Phase 1

Introduced:

- Control Plane
- CRUD APIs
- PostgreSQL
- Repository Pattern
- Service Layer

Workers only registered themselves.

No scheduling existed.

---

## Phase 2

Introduced:

- Heartbeats
- Worker health
- Background reconciler

Workers became "alive".

Still no scheduling.

---

## Phase 3

Introduced:

- Worker runtime
- Runtime process
- Polling infrastructure

Workers were now capable of communicating continuously.

Still no assignment mechanism existed.

---

## Phase 4

Introduced:

- Scheduler
- Assignment table
- Reservation algorithm
- Worker polling
- Runtime execution
- Assignment state transitions
- Automatic completion

This phase connects every subsystem built previously.

---

# 5. High-Level Scheduling Architecture

Scheduling consists of four major actors.

```
Client

↓

Control Plane

↓

Scheduler

↓

Worker
```

The flow begins when a user starts a test.

```
POST /tests/{id}/start
```

The scheduler then:

1. Finds available workers.
2. Locks them.
3. Reserves them.
4. Creates assignment records.
5. Returns worker details.

Workers eventually discover these assignments through polling.

This design avoids the need for sockets, WebSockets or message brokers.

The worker remains completely autonomous.

---

# 6. Component Responsibilities

## Control Plane

Responsible for:

- API layer
- validation
- scheduling
- worker reservation
- assignment tracking
- reconciliation

The Control Plane never performs the actual load test.

It only coordinates workers.

---

## Scheduler

Responsible for selecting workers.

The scheduler guarantees:

- no worker assigned twice
- no race conditions
- deterministic reservations
- transactional allocation

The scheduler is intentionally separated behind an interface.

```
Scheduler
    ↓
DefaultScheduler
```

This allows future schedulers to implement different strategies such as:

- Least Loaded
- Zone Aware
- Round Robin
- Weighted Scheduling

without changing business logic.

---

## Worker Runtime

Each worker continuously performs two independent background tasks.

Heartbeat Loop

```
Worker

↓

Heartbeat

↓

Control Plane
```

Poll Loop

```
Worker

↓

GET Assignment

↓

Execute

↓

Complete
```

Both loops run independently using Go goroutines.

Neither blocks the other.

---

## PostgreSQL

PostgreSQL acts as the system of record.

It stores:

- workers
- tests
- assignments

It does **not** store runtime metrics.

Metrics will be introduced in Phase 5.

---

# 7. Worker Lifecycle

Workers now follow a complete lifecycle.

```
Register

↓

IDLE

↓

Reserved

↓

Receive Assignment

↓

RUNNING

↓

Execution

↓

Assignment Completed

↓

IDLE

↓

Poll Again
```

The worker never exits after finishing one assignment.

Instead it returns to polling indefinitely.

This enables a small worker fleet to execute many tests sequentially.

---

# 8. Database Changes

The biggest architectural addition in Phase 4 is the introduction of the assignment table.

```
tests

↓

test_workers

↓

workers
```

Previously there was no relationship between tests and workers.

Now every assignment is explicitly recorded.

---

## test_workers Table

The schema contains:

| Column | Purpose |
|---------|----------|
| test_id | Test owning the assignment |
| worker_id | Assigned worker |
| status | RESERVED / RUNNING / COMPLETED |
| assigned_at | Reservation timestamp |
| started_at | Execution start |
| completed_at | Execution completion |

This table represents the lifecycle of every assignment.

It is **not** a history table.

It always reflects the current assignment state.

---

## Why a Composite Primary Key?

```
PRIMARY KEY
(
    test_id,
    worker_id
)
```

A worker should only appear once within a test.

Likewise, a test should not reserve the same worker twice.

The composite key guarantees both constraints.

---

## Foreign Keys

Two foreign keys are defined.

```
test_id

↓

tests(id)
```

and

```
worker_id

↓

workers(id)
```

Deleting a worker or test automatically removes assignment rows.

This prevents orphaned records.

---

# 9. Assignment Model

Each assignment has three possible states.

```
RESERVED

↓

RUNNING

↓

COMPLETED
```

These transitions are strictly linear.

```
RESERVED

↓

RUNNING

↓

COMPLETED
```

There is no direct transition from RESERVED to COMPLETED.

Likewise, a COMPLETED assignment cannot become RUNNING again.

This greatly simplifies recovery logic in future phases.

---

# 10. Scheduler Design

The scheduler follows a very small public interface.

```go
type Scheduler interface {
    AllocateWorkers(
        ctx context.Context,
        testID string,
        workerCount int,
    ) ([]models.Worker, error)
}
```

The implementation is intentionally hidden behind this interface.

Today only one implementation exists.

```
DefaultScheduler
```

Future versions may introduce Kubernetes-aware scheduling, zone-aware scheduling or cost-based allocation without changing any service layer code.

---

# 11. Worker Reservation Algorithm

The reservation algorithm is the heart of Phase 4.

It executes inside a single PostgreSQL transaction.

The sequence is:

```
BEGIN

↓

Find IDLE workers

↓

Lock rows

↓

Reserve workers

↓

Insert assignment rows

↓

Commit
```

Every step either succeeds together or fails together.

This guarantees consistency.

## Row Locking

Workers are selected using:

```sql
FOR UPDATE SKIP LOCKED
```

This is one of the most important SQL features used in the project.

It prevents two concurrent schedulers from reserving the same worker.

If Scheduler A has already locked a worker, Scheduler B simply skips it instead of waiting.

This makes scheduling highly concurrent while avoiding deadlocks.

---

# 12. Design Decisions

## Why polling instead of push?

Push-based systems require:

- WebSockets
- Message brokers
- gRPC streams

Polling keeps workers stateless and greatly simplifies failure recovery.

If a worker crashes, it simply resumes polling after restart.

---

## Why store assignments separately?

Embedding assignment information inside the `workers` table would have violated normalization and made historical tracking difficult.

A dedicated assignment table:

- supports multiple workers per test
- keeps worker metadata independent
- tracks assignment lifecycle cleanly
- prepares the project for future distributed execution

---

## Why use transactions?

Scheduling is an all-or-nothing operation.

Without transactions, failures could leave the system in inconsistent states—for example:

- worker marked RESERVED but no assignment row created
- assignment created but worker still IDLE

Wrapping the reservation flow in a single transaction guarantees atomicity and consistency.

---

# 13. Worker Runtime

One of the biggest milestones achieved in Phase 4 is the introduction of the **Worker Runtime**.

Prior to this phase, workers were nothing more than registered database entries. They had no autonomous behavior and required manual API calls to simulate work.

With the runtime in place, each worker becomes an independent process capable of:

- Registering itself with the Control Plane
- Maintaining its own heartbeat
- Polling for assignments
- Executing assigned work
- Reporting completion
- Returning to an idle state

The runtime is designed to run continuously until the process is terminated.

---

# 14. Runtime Architecture

The runtime consists of three major components.

```
Worker Runtime

├── Registration
├── Heartbeat Loop
└── Poll Loop
```

Registration happens exactly once.

The remaining two loops continue running throughout the lifetime of the process.

---

## Runtime Structure

```go
type Runtime struct {
    logger *slog.Logger
    worker *Worker
    wg     sync.WaitGroup
}
```

The runtime is intentionally lightweight.

It simply coordinates multiple background goroutines while handling graceful shutdown.

---

# 15. Runtime Startup

The lifecycle begins inside:

```go
Runtime.Start(ctx)
```

The startup sequence is:

```
Start Runtime

↓

Register Worker

↓

Launch Heartbeat Loop

↓

Launch Poll Loop

↓

Wait for Shutdown Signal
```

Both background loops execute independently.

```
            Runtime

               |

     --------------------

     |                  |

HeartbeatLoop      PollLoop

     |                  |

Every 5s          Every 3s
```

Neither goroutine blocks the other.

---

# 16. Registration

When the runtime starts, the first responsibility is registration.

```
Worker

↓

POST /workers

↓

Control Plane

↓

Persist Worker

↓

Return Worker ID
```

The worker stores the returned ID locally.

```go
w.id = response.ID
```

This identifier is used for every subsequent API call.

Without registration, the runtime cannot function.

---

# 17. Heartbeat Loop

Once registration succeeds, the runtime launches the heartbeat goroutine.

```
go HeartbeatLoop()
```

Its only responsibility is to periodically inform the Control Plane that the worker is still alive.

```
Worker

↓

Heartbeat

↓

Control Plane

↓

Update last_heartbeat
```

The heartbeat loop does **not** ask for work.

It only updates health information.

---

## Heartbeat Interval

The loop uses a ticker.

```go
ticker := time.NewTicker(...)
```

Every tick:

```
Ticker

↓

sendHeartbeat()

↓

Sleep

↓

Repeat
```

The interval is configurable.

Typical configuration:

```
5 seconds
```

---

## Heartbeat Payload

Each heartbeat transmits the current worker status.

Example:

```json
{
    "status":"IDLE"
}
```

Possible values include:

- IDLE
- RESERVED
- RUNNING
- DRAINING
- OFFLINE

Although only a subset is actively used in Phase 4, the additional states prepare the system for future scheduling strategies.

---

## Heartbeat Processing

The Control Plane performs the following actions:

```
Receive Request

↓

Validate Worker

↓

Update Status

↓

Update last_heartbeat

↓

Return 204
```

No response body is required.

---

# 18. Poll Loop

The second goroutine is responsible for discovering assignments.

Unlike the heartbeat loop, polling retrieves work.

```
Worker

↓

GET Assignment

↓

Receive Assignment?

↓

Yes

↓

Execute

↓

Complete

↓

Poll Again
```

If no assignment exists:

```
Worker

↓

GET Assignment

↓

No Assignment

↓

Sleep

↓

Retry
```

The worker never stops polling.

---

## Poll Interval

Polling is intentionally more frequent than heartbeats.

Example:

```
Heartbeat

Every 5 seconds

Polling

Every 3 seconds
```

This allows newly started tests to begin quickly while avoiding excessive heartbeat traffic.

---

# 19. Assignment Discovery

The worker periodically invokes:

```
GET

/workers/{id}/assignment
```

Possible responses:

### No Assignment

```json
{
    "assigned": false
}
```

The worker simply waits for the next polling cycle.

---

### Assignment Available

```json
{
    "assigned": true,
    "test_id":"...",
    "status":"RESERVED"
}
```

The runtime immediately begins processing.

---

# 20. Assignment Processing

Once an assignment is discovered, the following sequence occurs.

```
Receive Assignment

↓

Start Assignment API

↓

Execute

↓

Complete Assignment API

↓

Return to Polling
```

The runtime performs all of these steps automatically.

No manual intervention is required.

---

# 21. Starting an Assignment

Before beginning execution, the worker notifies the Control Plane.

```
POST

/workers/{id}/assignment/start
```

The Control Plane updates:

```
Status

RESERVED

↓

RUNNING
```

It also records:

```
started_at
```

inside the assignment table.

---

## Why is this necessary?

Without this transition, the scheduler would have no idea whether:

- a worker has merely received work
- or has actually started executing it

Tracking these states becomes essential for timeout detection in future phases.

---

# 22. Execution

At this stage the runtime invokes:

```go
execute()
```

In Phase 4, execution is intentionally simulated.

The implementation simply sleeps.

```
Execute

↓

Sleep

↓

Return
```

Example:

```go
time.Sleep(10 * time.Second)
```

This placeholder will later be replaced by the real load generation engine.

---

## Why simulate execution?

The objective of Phase 4 is scheduling.

Not load generation.

Keeping execution simple allows us to validate the scheduling pipeline independently.

---

# 23. Completing an Assignment

After execution finishes, the worker informs the Control Plane.

```
POST

/workers/{id}/assignment/complete
```

The Control Plane updates:

```
RUNNING

↓

COMPLETED
```

and records

```
completed_at
```

The assignment is now considered finished.

---

# 24. Returning to Idle

Once completion succeeds:

```
Assignment Finished

↓

Worker Status

↓

IDLE

↓

Resume Polling
```

The runtime immediately becomes available for another assignment.

No restart is required.

---

# 25. Runtime Concurrency Model

The runtime contains two continuously executing goroutines.

```
Runtime

↓

---------------------------

Heartbeat Loop

Poll Loop

---------------------------
```

Both loops communicate with the Control Plane independently.

They only share immutable worker configuration.

This design avoids synchronization issues and keeps locking requirements minimal.

---

# 26. Graceful Shutdown

The runtime uses a shared context.

```
Context Cancelled

↓

Heartbeat Stops

↓

Poll Stops

↓

WaitGroup Completes

↓

Runtime Exits
```

Every background goroutine listens for:

```go
ctx.Done()
```

When cancellation occurs:

- tickers stop
- loops exit
- WaitGroup reaches zero
- Start() returns

This ensures clean termination without leaking goroutines.

---

# 27. Worker Reconciler

The reconciler executes inside the Control Plane.

Unlike polling or heartbeats, it is **not** triggered by workers.

It runs on a fixed schedule.

```
Timer

↓

Find Stale Workers

↓

Mark OFFLINE
```

---

## Purpose

Workers may crash unexpectedly.

If this happens, heartbeats stop arriving.

Without reconciliation, workers would remain permanently marked as `IDLE`.

This could lead to scheduling dead workers.

The reconciler prevents that.

---

## Algorithm

Every interval:

```
Current Time

↓

Subtract Timeout

↓

Cutoff Time

↓

Workers Older Than Cutoff?

↓

Mark OFFLINE
```

Workers exceeding the heartbeat timeout are updated atomically.

---

# 28. Why Heartbeats and Polling are Separate

Although both are periodic background tasks, they serve different purposes.

| Heartbeat | Polling |
|------------|----------|
| Reports health | Requests work |
| Updates timestamps | Retrieves assignments |
| Runs every few seconds | Runs every few seconds |
| Independent of execution | May trigger execution |

Separating these concerns keeps the runtime simpler and avoids coupling unrelated responsibilities.

---

# 29. Complete Runtime Sequence Diagram

```text
Runtime

│

├── Register Worker

│

├── Start Heartbeat Loop

│

├── Start Poll Loop

│

├─────────────┐

│             │

│       GET Assignment

│             │

│     Assignment?

│

│      No ───────────────┐

│                        │

│<───────────────────────┘

│

│ Yes

│

├── POST Assignment Start

│

├── Execute

│

├── POST Assignment Complete

│

└── Continue Polling
```

This loop continues until the worker process is terminated.

---

# 30. Phase 4 Runtime Summary

At the completion of Phase 4, every worker is capable of autonomously participating in the distributed scheduling system.

The worker:

- registers itself
- maintains its heartbeat
- discovers work through polling
- transitions assignments through their lifecycle
- simulates execution
- reports completion
- returns to the idle pool
- waits for the next assignment

This completes the end-to-end scheduling pipeline introduced in Phase 4.

---

# 31. Repository Layer

The repository layer is responsible for **all database interactions**.

No SQL queries are written inside handlers or services.

Instead, every database operation is centralized inside repositories.

The architecture follows:

```
HTTP Request

↓

Handler

↓

Service

↓

Repository

↓

PostgreSQL
```

This separation keeps responsibilities clear.

---

# 32. Worker Repository

The `WorkerRepository` manages all operations related to workers and assignments.

Major responsibilities include:

- Registering workers
- Fetching workers
- Updating heartbeats
- Marking workers offline
- Reserving workers
- Releasing workers
- Retrieving assignments
- Transitioning assignment state

Current interface:

```go
type WorkerRepository interface {
    CreateWorker(...)
    GetWorkers(...)
    GetWorkerByID(...)
    UpdateHeartbeat(...)
    MarkOfflineWorkers(...)

    ReserveWorkersForTest(...)
    ReleaseWorkersForTest(...)

    GetReservedAssignment(...)
    MarkAssignmentRunning(...)
    MarkAssignmentCompleted(...)
}
```

Every scheduling operation ultimately passes through this repository.

---

# 33. Reserving Workers

Worker reservation is one of the most critical pieces of Phase 4.

The scheduler requests:

```
Reserve N workers
```

The repository performs:

```
BEGIN

↓

SELECT ...

FOR UPDATE SKIP LOCKED

↓

UPDATE workers

↓

INSERT assignments

↓

COMMIT
```

Everything occurs inside a single transaction.

---

## Why a transaction?

Without a transaction:

```
Scheduler A

↓

Select Worker

↓

Pause

↓

Scheduler B

↓

Select Same Worker
```

Both schedulers could reserve the same worker.

Using a transaction eliminates this race condition.

---

# 34. Releasing Workers

When a test stops:

```
ReleaseWorkersForTest()
```

performs:

```
BEGIN

↓

Find Assignments

↓

Update Worker Status

↓

Delete Assignment Rows

↓

COMMIT
```

The workers immediately become available for future scheduling.

---

# 35. Assignment Repository Operations

Assignments progress through three major states.

```
RESERVED

↓

RUNNING

↓

COMPLETED
```

Repository functions implement these transitions.

---

## MarkAssignmentRunning

Updates:

```
status

↓

RUNNING
```

and records

```
started_at
```

---

## MarkAssignmentCompleted

Updates:

```
status

↓

COMPLETED
```

and records

```
completed_at
```

These timestamps become useful later for execution metrics.

---

# 36. Service Layer

The service layer contains business logic.

Handlers should never make scheduling decisions.

Instead:

```
Handler

↓

Service

↓

Repository
```

Examples include:

```
WorkerService

TestService

Scheduler
```

Each service performs validation before interacting with the database.

---

# 37. Scheduler

The scheduler acts as the orchestration layer.

Its responsibility is simple:

```
Request Workers

↓

Reserve Workers

↓

Return Allocation
```

Current implementation:

```
DefaultScheduler

↓

WorkerRepository

↓

ReserveWorkersForTest()
```

Although intentionally lightweight in Phase 4, this abstraction allows more advanced scheduling algorithms in future phases.

---

# 38. Assignment Lifecycle

Every assignment follows a well-defined lifecycle.

```
Worker Reserved

↓

Assignment Created

↓

Worker Polls

↓

RUNNING

↓

Execution

↓

COMPLETED
```

Each transition is explicitly recorded.

No implicit state changes occur.

---

# 39. Worker Status Lifecycle

Worker status is independent from assignment status.

Worker state:

```
IDLE

↓

RESERVED

↓

RUNNING

↓

IDLE
```

If the worker disappears:

```
IDLE

↓

OFFLINE
```

The reconciler is responsible for this transition.

---

# 40. API Endpoints

The following endpoints exist at the completion of Phase 4.

---

## Worker APIs

### Register Worker

```
POST

/api/v1/workers
```

Request

```json
{
  "hostname":"worker-1",
  "version":"v1.0.0",
  "cpu_count":8,
  "memory_mb":16384
}
```

Response

```json
{
  "id":"...",
  "hostname":"worker-1",
  "status":"IDLE"
}
```

---

### List Workers

```
GET

/api/v1/workers
```

Returns all registered workers.

---

### Heartbeat

```
POST

/api/v1/workers/{id}/heartbeat
```

Request

```json
{
    "status":"IDLE"
}
```

Response

```
204 No Content
```

---

### Get Assignment

```
GET

/api/v1/workers/{id}/assignment
```

No assignment:

```json
{
    "assigned":false
}
```

Assignment exists:

```json
{
    "assigned":true,
    "test_id":"...",
    "status":"RESERVED"
}
```

---

### Assignment Start

```
POST

/api/v1/workers/{id}/assignment/start
```

Updates assignment status to RUNNING.

Returns

```
204 No Content
```

---

### Assignment Complete

```
POST

/api/v1/workers/{id}/assignment/complete
```

Updates assignment status to COMPLETED.

Returns

```
204 No Content
```

---

# 41. Test APIs

### Create Test

```
POST

/api/v1/tests
```

Request

```json
{
    "name":"Runtime Test",
    "worker_count":1
}
```

Response

```json
{
    "ID":"...",
    "Status":"CREATED"
}
```

---

### Start Test

```
POST

/api/v1/tests/{id}/start
```

Example Response

```json
{
    "test_id":"...",
    "status":"RUNNING",
    "workers":[
        {
            "id":"worker-id",
            "hostname":"worker-1"
        }
    ]
}
```

---

### Stop Test

```
POST

/api/v1/tests/{id}/stop
```

Releases every reserved worker.

Returns

```
204 No Content
```

---

### Get Test

```
GET

/api/v1/tests/{id}
```

Returns metadata about the test.

---

### List Tests

```
GET

/api/v1/tests
```

Returns all tests.

---

# 42. Error Handling

The API returns consistent error responses.

Example

```json
{
    "error":{
        "code":"NOT_FOUND",
        "message":"worker not found"
    }
}
```

Typical error codes include:

```
VALIDATION_ERROR

NOT_FOUND

INTERNAL_SERVER_ERROR

INSUFFICIENT_WORKERS
```

This structure is shared across all handlers.

---

# 43. Logging

Phase 4 introduced structured logging throughout both components.

Example worker log:

```
worker registered

heartbeat sent

assignment received

assignment completed
```

Example server log:

```
worker reconciler started

test started

worker reserved

worker released
```

Structured logging makes debugging significantly easier than plain text logs.

---

# 44. Folder Structure (Relevant Components)

```
internal/

api/
    handlers/

repository/

service/

scheduler/

worker/

controlplane/

models/

runtime/
```

Responsibilities:

```
handlers

↓

HTTP

service

↓

Business Logic

repository

↓

Database

worker

↓

Runtime

controlplane

↓

HTTP Client
```

---

# 45. Phase 4 Achievements

At the end of this phase, Vulcan now supports:

✓ Worker registration

✓ Heartbeats

✓ Offline detection

✓ Scheduler abstraction

✓ Worker reservation

✓ Atomic assignment creation

✓ Worker runtime

✓ Assignment polling

✓ Assignment execution lifecycle

✓ Assignment completion

✓ Graceful shutdown

✓ Runtime integration testing

This represents the first complete end-to-end distributed scheduling workflow.

---

# 46. Remaining Limitations

Although Phase 4 is feature complete, several limitations remain by design.

Current execution:

```
time.Sleep(10s)
```

instead of real load generation.

Assignments are still:

- database driven
- poll based
- single-node

There is:

- no message broker
- no telemetry
- no metrics aggregation
- no distributed event streaming

These are intentional and become the focus of Phase 5.

---

# 47. Transition to Phase 5

Phase 4 answered one question:

> **Which worker should execute this test?**

Phase 5 answers the next one:

> **How do hundreds of workers stream millions of metrics back to the Control Plane efficiently?**

This marks the transition from **distributed scheduling** to the **distributed telemetry pipeline**.

The next phase introduces:

- Worker metric generation
- Local metric aggregation
- NATS messaging
- Metrics Aggregator service
- VictoriaMetrics integration
- Global RPS/Latency/Error Rate calculation
- Real-time observability

Phase 5 transforms Vulcan from a scheduler into a true distributed load-testing platform.

---
