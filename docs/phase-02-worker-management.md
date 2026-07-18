# Vulcan - Phase 2 Documentation

# Worker Management

---

# Objective

The goal of Phase 2 was to transform the Control Plane from a simple CRUD API into an actual cluster manager.

Instead of only storing worker information, the Control Plane now maintains worker lifecycle, health and availability automatically.

This phase introduces one of the most fundamental distributed systems concepts:

> Reconciliation.

---

# What Was Built

## Worker Registration

Workers can now register themselves with the Control Plane.

Registration stores immutable metadata including:

- Worker ID (ULID)
- Hostname
- Version
- CPU Count
- Memory
- Registration Timestamp

Initial status:

```
IDLE
```

---

## Worker Lifecycle

Worker states:

```
IDLE
RESERVED
RUNNING
DRAINING
OFFLINE
```

Meaning:

### IDLE

Worker is healthy and available.

### RESERVED

Worker has been allocated to a test but has not started execution.

### RUNNING

Worker is actively generating traffic.

### DRAINING

Worker finishes current work but should not receive new work.

### OFFLINE

Worker missed heartbeat timeout.

---

# Heartbeat System

Workers periodically send heartbeats.

API:

```
POST /api/v1/workers/{id}/heartbeat
```

Payload:

```json
{
    "status": "IDLE"
}
```

Heartbeat updates:

- status
- last_heartbeat
- updated_at

Workers are allowed to report:

- IDLE
- RESERVED
- RUNNING
- DRAINING

Workers cannot directly report:

- OFFLINE

OFFLINE is determined only by the Control Plane.

---

# Reconciliation Loop

This is the biggest addition of Phase 2.

A background controller continuously verifies worker health.

Loop:

Every 5 seconds:

```
Find workers

↓

last_heartbeat older than 15 seconds

↓

Mark OFFLINE
```

Implementation:

```
internal/reconciler/
    worker_reconciler.go
```

Structure:

```
WorkerReconciler

↓

Repository

↓

PostgreSQL
```

The reconciler is independent of HTTP handlers.

---

# Why Reconciliation Exists

If a worker crashes:

```
Worker

↓

Stops sending heartbeats

↓

Cannot notify Control Plane

↓

Control Plane infers failure

↓

Worker becomes OFFLINE
```

This follows production control-plane design.

---

# Timing

Heartbeat interval:

```
5 seconds
```

Worker timeout:

```
15 seconds
```

Meaning:

Workers may miss two heartbeats before being considered dead.

---

# Repository Additions

Added:

```
CreateWorker()

GetWorkers()

GetWorkerByID()

UpdateHeartbeat()

MarkOfflineWorkers()
```

MarkOfflineWorkers performs:

```
UPDATE workers
SET status='OFFLINE'
WHERE last_heartbeat < cutoff
```

Returns:

```
Rows affected
```

Zero affected rows are NOT treated as errors.

---

# API Endpoints

## Register Worker

```
POST /api/v1/workers
```

Example:

```json
{
    "hostname":"worker-1",
    "version":"v1.0.0",
    "cpu_count":8,
    "memory_mb":16384
}
```

Returns:

```
201 Created
```

---

## List Workers

```
GET /api/v1/workers
```

---

## Get Worker

```
GET /api/v1/workers/{id}
```

---

## Heartbeat

```
POST /api/v1/workers/{id}/heartbeat
```

Example:

```json
{
    "status":"IDLE"
}
```

Returns:

```
204 No Content
```

---

# Architecture

```
                HTTP

                  │

             WorkerHandler

                  │

             WorkerService

                  │

          WorkerRepository

                  │

             PostgreSQL

────────────────────────────────

        WorkerReconciler

                  │

          WorkerRepository

                  │

             PostgreSQL
```

The Control Plane now contains:

- Request/Response APIs
- Background Controllers

---

# Testing Performed

Worker Registration

✓ Register worker

✓ Verify database

Worker Retrieval

✓ List workers

✓ Fetch by ID

Heartbeat

✓ Heartbeat updates timestamp

✓ Invalid status validation

✓ Unknown worker handling

Reconciliation

✓ Worker becomes OFFLINE after heartbeat timeout

✓ Healthy workers remain IDLE

✓ Background reconciliation logs only when state changes

---

# Design Decisions

## ULID

Worker IDs use ULID instead of UUID.

Reasons:

- Lexicographically sortable
- Time ordered
- Human readable
- Same strategy as Tests

---

## No Worker Can Mark Itself OFFLINE

Reason:

A crashed worker cannot send one final request.

The Control Plane owns worker health.

---

## Background Reconciler

Implemented separately from Services.

Reason:

Reconciliation is autonomous infrastructure, not request-driven business logic.

---

## No Generic Repository

Repository methods remain explicit.

Example:

```
MarkOfflineWorkers()
```

instead of generic update functions.

---

# Concepts Learned

Control Plane

Worker Lifecycle

Heartbeats

Health Checking

Reconciliation

Background Controllers

Tickers

Context Cancellation

Long-running Goroutines

Failure Detection

Eventually Consistent State

Distributed Systems Control Loops

---

# Current Project Status

Completed

✓ Phase 0
Architecture

✓ Phase 1
Control Plane Foundation

✓ Phase 2
Worker Management

Next

Phase 3

Worker Scheduling

Topics:

- Worker Reservation
- Atomic Scheduling
- PostgreSQL Transactions
- Row-Level Locking
- FOR UPDATE SKIP LOCKED
- Scheduler Design