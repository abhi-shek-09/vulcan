# Architecture

Vulcan is three Go binaries built from one module, sharing a Postgres
database and a NATS bus, sitting in front of VictoriaMetrics/Grafana for
metrics.

```
                         ┌─────────────────────┐
                         │      PostgreSQL      │
                         │ tests / workers /    │
                         │ test_workers         │
                         └──────────▲───────────┘
                                    │
 REST clients ──────────► ┌────────┴─────────┐        provisions/terminates
   (curl, UI, CI)         │  Control Plane    ├───────────► worker processes
                          │  cmd/server        │            (ProcessProvisioner)
                          │                    │
                          │ - HTTP API (chi)   │
                          │ - Scheduler        │
                          │ - Autoscaler       │
                          │ - Worker reconciler│
                          └──────────┬─────────┘
                                     │ assignment polling / heartbeats
                                     ▼
                         ┌───────────────────────┐        HTTP traffic
                         │       Worker(s)        ├────────────────────►  target
                         │       cmd/worker        │                       under test
                         │ - poll loop            │
                         │ - execution engine      │
                         │ - metrics publisher     │
                         └───────────┬─────────────┘
                                     │ metrics.test.<id>  (NATS)
                                     ▼
                         ┌───────────────────────┐
                         │      Aggregator         │
                         │      cmd/aggregator      │
                         │ - subscribes to NATS     │
                         │ - merges per-worker      │
                         │   buckets into a global  │
                         │   window per test        │
                         │ - flushes periodically   │
                         └───────────┬─────────────┘
                                     ▼
                         ┌───────────────────────┐
                         │    VictoriaMetrics      │
                         └──────────┬────────────┘
                                    │
                       ┌────────────┼─────────────┐
                       ▼                           ▼
                 Grafana dashboards        Control-plane dashboard API
                                            (GET /tests/{id}/metrics[/history])
```

## Components

### Control plane (`cmd/server`, `internal/api`, `internal/service`, `internal/scheduler`, `internal/autoscaler`, `internal/reconciler`)

The only component with a Postgres connection and the only one exposing an
HTTP API. On startup it wires up (see `cmd/server/main.go`):

- **`api/router`** — a `chi` router exposing `/health` and `/api/v1/...`.
- **`service.TestService`** — validates and creates tests, computes worker
  counts via `scheduler.RequiredWorkers`, and drives the
  `CREATED → STARTING → RUNNING → (STOPPING → STOPPED | COMPLETED | FAILED)`
  transitions.
- **`service.WorkerService`** — worker registration, heartbeats, and
  assignment polling/transition endpoints consumed by workers.
- **`scheduler.DefaultScheduler`** — turns "N workers, R total RPS" into
  concrete worker reservations and per-worker RPS allocations
  (`scheduler.DistributeRPS`).
- **`autoscaler.Reconciler`** — the single source of truth for fleet size;
  see [`autoscaling.md`](autoscaling.md).
- **`reconciler.WorkerReconciler`** — a background loop that detects
  workers which have stopped heartbeating and marks them `OFFLINE`.
- **`provisioner.ProcessProvisioner`** — starts/stops worker **processes**
  (`os/exec`, not containers) when the autoscaler needs more or less
  capacity.
- **`dashboard.Service`** — queries VictoriaMetrics for the live/history
  metrics endpoints.

### Worker (`cmd/worker`, `internal/worker`, `internal/execution`, `internal/controlplane`)

A worker is a standalone process that:

1. Registers with the control plane (`POST /api/v1/workers`) with retry and
   exponential backoff (`worker.Runtime.registerWithRetry`), since the
   control plane may not be ready yet at container start.
2. Runs two concurrent loops (`worker.Runtime.Start`):
   - **Heartbeat loop** — periodically reports liveness and its own view
     of its status.
   - **Poll loop** — periodically asks the control plane
     (`GET /api/v1/workers/{id}/assignment`) whether it has been assigned a
     test.
3. On receiving an assignment, marks it `RUNNING`
   (`POST .../assignment/start`), then runs the load test via
   `execution.Engine`, which schedules requests at the target RPS
   (`execution.Scheduler`) and issues them with `execution.HTTPExecutor`.
4. Publishes per-window metric buckets to NATS (`metrics.Publisher`) as it
   runs.
5. Reports completion or failure back to the control plane
   (`.../assignment/complete` or `.../assignment/fail`), or, if it detects
   the test was stopped externally, exits the execution loop cleanly
   without double-reporting a terminal state the control plane already
   recorded.

### Aggregator (`cmd/aggregator`, `internal/aggregator`, `internal/victoria`)

A stateless-at-rest process (state lives only in memory between flushes)
that:

1. Subscribes to `metrics.test.*` on NATS.
2. Merges every incoming `metrics.MetricBucket` into an in-memory
   `GlobalWindow` per test (`aggregator.Store`), tracking totals, averages,
   max latency, and the set of contributing worker IDs.
3. On a fixed interval, snapshots and resets each window, then writes the
   snapshot to VictoriaMetrics via `internal/victoria.Writer` and logs a
   summary line.

### Data stores

- **PostgreSQL** — the system of record for `tests`, `workers`, and the
  `test_workers` join table (assignment state). See `migrations/` for the
  schema history.
- **NATS** — a lightweight, at-most-once transport for telemetry. Subjects
  are `metrics.test.<test_id>`; no persistence/JetStream is used, since a
  dropped metric bucket only affects one aggregation window, not
  correctness of the test itself.
- **VictoriaMetrics** — durable time-series storage for everything the
  aggregator flushes; queried by both Grafana and the control plane's own
  dashboard endpoints.

## API surface

All routes are under `/api/v1` unless noted. Full handler code is in
`internal/api/handlers`.

| Method | Path | Purpose |
|---|---|---|
| GET | `/health` | Liveness check for the control plane |
| POST | `/workers` | Register a new worker |
| GET | `/workers` | List all workers |
| GET | `/workers/{id}` | Get one worker |
| POST | `/workers/{id}/heartbeat` | Worker liveness + status report |
| GET | `/workers/{id}/assignment` | Worker polls for its current assignment |
| POST | `/workers/{id}/assignment/start` | Worker reports it started executing |
| POST | `/workers/{id}/assignment/complete` | Worker reports successful completion |
| POST | `/workers/{id}/assignment/fail` | Worker reports execution failure |
| POST | `/tests` | Create a test (validates and computes worker count) |
| GET | `/tests` | List all tests |
| GET | `/tests/{id}` | Get one test |
| POST | `/tests/{id}/start` | Reserve/provision workers and start the test |
| POST | `/tests/{id}/stop` | Stop a running or pending test |
| GET | `/tests/{id}/metrics` | Live metrics snapshot for a test |
| GET | `/tests/{id}/metrics/history?start=&end=&step=` | Historical time series for a test |

`CreateTest` requires `name`, `target_url`, `method` (`GET`/`POST`),
`duration_sec > 0`, `rps > 0`, and `concurrency > 0`; the worker count is
derived, not supplied by the caller — see
`scheduler.RequiredWorkers(rps, workerCapacityRPS)`
(`ceil(rps / workerCapacityRPS)`).
