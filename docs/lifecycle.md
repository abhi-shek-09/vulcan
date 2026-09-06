# Lifecycle & State Machines

Vulcan has three interlocking state machines: the **test**, the **worker**,
and the **assignment** (the row joining a specific test to a specific
worker, in `test_workers`). Keeping them separate is what lets, for
example, one worker fail an assignment without corrupting the state of
every other worker running the same test.

## Test lifecycle

```
CREATED ──start──► STARTING ──(workers reserved)──► RUNNING
   │                                                    │
   │                                                    ├── stop ──► STOPPING ──► STOPPED
   │                                                    │
   │                                                    ├── all workers COMPLETED ──► COMPLETED
   │                                                    │
   │                                                    └── unrecoverable failure ──► FAILED
   │
   └── stop ──► STOPPING ──► STOPPED
```

States (`internal/constants/status.go`, `internal/models/tests.go`):
`CREATED`, `STARTING`, `RUNNING`, `STOPPING`, `STOPPED`, `COMPLETED`,
`FAILED`.

- **`CreateTest`** (`service.TestService.CreateTest`) validates the
  request, computes `WorkerCount = ceil(RPS / WorkerCapacityRPS)`
  (`scheduler.RequiredWorkers`), and inserts the test as `CREATED` with
  that worker count already stamped on it — even though no worker is
  reserved yet. This number is what the autoscaler's *pending capacity*
  calculation uses (see [`autoscaling.md`](autoscaling.md)).
- **`StartTest`** (`service.TestService.StartTest`) is the only place a
  test leaves `CREATED`:
  1. Atomically claims the transition `CREATED → STARTING` with a single
     conditional `UPDATE ... WHERE status = 'CREATED'`. If two `start`
     requests race, only one succeeds; the other gets
     `ErrInvalidTestState` immediately, before any worker is touched.
  2. Recomputes the required worker count and calls
     `ensureWorkersAvailable`, which checks currently-`IDLE` workers and,
     if short, delegates to the autoscaler's reconciler (or the
     provisioner directly, if no reconciler is wired) to provision the
     shortfall, then polls until enough workers are `IDLE` or a 30s
     timeout elapses.
  3. Distributes total RPS across workers (`scheduler.DistributeRPS`) and
     reserves them (`scheduler.AllocateWorkers`), which flips each chosen
     worker to `RESERVED` and inserts one `test_workers` row per worker
     with assignment status `RESERVED`.
  4. On any failure in steps 2–3, rolls the test back to `CREATED` so the
     caller can retry `start` instead of it being stuck in `STARTING`
     forever.
  5. On success, flips the test to `RUNNING`.
- **`StopTest`** works from `CREATED`, `STARTING`, or `RUNNING`: it's a
  no-op if the test is already `STOPPED`/`COMPLETED`/`FAILED`, otherwise it
  transitions to `STOPPING`, releases all of the test's workers back to
  `IDLE` (`ReleaseWorkersForTest`), and then marks the test `STOPPED`.
  Workers currently executing that test detect `STOPPING`/`STOPPED` the
  next time they poll and cancel their in-flight execution instead of
  running to completion (`worker.ErrAssignmentStopped`).
- **`COMPLETED`/`FAILED`** are reached via the assignment-completion path
  below, or when the background worker reconciler detects a dead worker
  mid-test and can no longer let its assignment finish.

## Worker lifecycle

```
             register
                │
                ▼
              IDLE ◄──────────────────────┐
             /  │  \                       │
   reserved /   │   \ no heartbeat          │ assignment released /
  for test /    │    \ (>120s)              │ completed / stopped
          ▼     │     ▼                     │
      RESERVED  │   OFFLINE                 │
          │     │                           │
   assignment   │                           │
    starts      │                           │
          ▼     │                           │
       RUNNING ─┴───────────────────────────┘
          │
          │  scale-down (idle + autoscaler-managed only)
          ▼
      DRAINING (terminal — process is being torn down)
```

States (`internal/models/workers.go`): `IDLE`, `RESERVED`, `RUNNING`,
`DRAINING`, `OFFLINE`.

- A worker registers itself (`POST /workers`) starting as `IDLE`.
- The scheduler flips it to `RESERVED` when a test claims it
  (`ReserveWorkersForTest`), and to `RUNNING` when the worker itself
  reports it has started executing (`MarkAssignmentRunning`, called from
  `worker.processAssignment` via `StartAssignment`).
- When the assignment finishes (success, failure, or external stop), the
  worker returns to `IDLE` — either because the worker sets its own status
  back after `CompleteAssignment`/`FailAssignment`, or because
  `StopTest`/`ReleaseWorkersForTest` did it on the test's behalf.
- **Heartbeats are advisory, not authoritative, for `RESERVED`/`DRAINING`/
  `OFFLINE`.** `UpdateHeartbeat` only ever lets a heartbeat overwrite
  `IDLE`/`RUNNING` with `IDLE`/`RUNNING`; it always refreshes
  `last_heartbeat` for liveness tracking, but a worker's own stale local
  belief about its status can never un-reserve or un-drain it. This closes
  a race uncovered in Phase 10 (see [`autoscaling.md`](autoscaling.md)).
- A background loop (`reconciler.WorkerReconciler`, ticking every 5s) marks
  any worker whose `last_heartbeat` is older than 120s as `OFFLINE`
  (`MarkOfflineWorkers`), then fails any assignment that can't possibly
  complete because its worker just went offline
  (`MarkAssignmentsFailedForOfflineWorkers`), which in turn can push the
  parent test toward `FAILED`.
- `DRAINING` is only ever set by the autoscaler's scale-down path
  (`autoscaler.Reconciler.scaleDown`), only on workers that are currently
  `IDLE` **and** autoscaler-managed (hostname prefixed
  `vulcan-auto-worker-`). It is effectively terminal: once
  `ProcessProvisioner.Terminate` is told to kill the process, the row is
  not returned to `IDLE` even if termination fails, since a worker that's
  supposed to be gone must never be handed back to the scheduler.

## Assignment lifecycle (`test_workers` row)

```
RESERVED ──worker starts──► RUNNING ──worker completes──► COMPLETED
   │                            │
   │                            └──worker fails / goes offline──► FAILED
   │
   └──worker never starts (offline before starting)──► FAILED
```

States are plain strings on `test_workers.status`: `RESERVED`, `RUNNING`,
`COMPLETED`, `FAILED` (`internal/repository/assignment.go`).

- Created as `RESERVED` at the same time the worker itself is reserved
  (`ReserveWorkersForTest`, one row per allocated worker, carrying its
  slice of the test's total RPS).
- A worker transitions its own assignment `RESERVED → RUNNING`
  (`MarkAssignmentRunning`) right before it starts executing, and
  `RUNNING → COMPLETED` (`MarkAssignmentCompleted`) right after it
  finishes successfully.
- `MarkAssignmentFailed` accepts `RESERVED` or `RUNNING` as valid
  starting states — a worker can fail before ever starting (e.g. it
  couldn't reach the target at all) or mid-run.
- `MarkAssignmentsFailedForOfflineWorkers` performs the same
  `RESERVED|RUNNING → FAILED` transition in bulk for every assignment
  belonging to a worker the health reconciler just marked `OFFLINE`.
- All of these are conditional `UPDATE ... WHERE status = '<expected>'`
  statements, mirroring the same "atomic claim, not check-then-act"
  pattern `StartTest` uses at the test level — a worker double-reporting
  completion, or reporting completion after already being marked failed,
  is a no-op rather than a corruption.

## Worker runtime loops

Independent of the state machines above, each worker process runs two
concurrent loops for its entire lifetime (`worker.Runtime.Start`):

- **Heartbeat loop** — reports liveness and current status on a fixed
  interval.
- **Poll loop** — asks `GET /workers/{id}/assignment` on a fixed interval;
  if assigned, runs `processAssignment` synchronously (a worker processes
  one assignment at a time).

Both loops exit cleanly on context cancellation (SIGINT/SIGTERM), and
`Runtime.Start` waits for both to finish before returning.
