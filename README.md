# Vulcan

**Vulcan** is a distributed load-testing platform written in Go. You describe
a test (target URL, RPS, duration, concurrency), Vulcan works out how many
workers it needs, provisions them if necessary, spreads the traffic across
the fleet, and streams live and historical metrics back through
VictoriaMetrics and Grafana.

It was built in ten phases, each one adding a layer of the system on top of
a working previous phase: control plane → worker registration → scheduling
→ worker runtime → telemetry pipeline → observability → failure resilience
→ fleet orchestration → autoscaling → containerized deployment. The
phase-by-phase write-ups live in [`docs/`](docs/index.md).

---

## What it does

```
Create load test
      |
      v
Autoscaler provisions / reserves workers
      |
      v
Workers generate real HTTP traffic against the target
      |
      v
Telemetry published over NATS
      |
      v
Aggregator merges per-worker buckets into global windows
      |
      v
VictoriaMetrics (time-series storage)
      |
      v
Grafana dashboards  /  REST API (live + historical metrics)
      |
      v
Test completes -> workers return to IDLE
```

## Features

- **Declarative load tests** — POST a target URL, method, RPS, concurrency,
  and duration; Vulcan calculates the worker count for you.
- **Fleet autoscaling** — a reconciler continuously compares worker supply
  against test demand (active *and* pending) and provisions or drains
  workers accordingly, without racing itself or the on-demand path
  triggered by starting a test.
- **Crash-safe worker lifecycle** — heartbeats, reconciliation of dead
  workers, and status transitions that can't be clobbered by stale
  heartbeat writes.
- **Real HTTP execution** — workers issue actual HTTP requests at a
  scheduled rate and report latency, success/failure, and byte counts.
- **Streaming telemetry** — workers publish metric buckets over NATS; an
  aggregator merges them into per-test windows and writes them to
  VictoriaMetrics on a fixed interval.
- **Live + historical metrics API** — `GET /tests/{id}/metrics` for the
  current snapshot, `GET /tests/{id}/metrics/history` for a time range.
- **Grafana dashboards** — provisioned out of the box against
  VictoriaMetrics.
- **One-command deployment** — the whole stack (Postgres, NATS,
  VictoriaMetrics, Grafana, control plane, a baseline worker, and the
  aggregator) comes up with a single `docker compose up -d --build`.

## Architecture

```
Control Plane (HTTP API, scheduler, autoscaler) ---- PostgreSQL
        |
        | assigns tests, tracks worker fleet
        v
   Worker(s)  --------------------------------->  NATS (telemetry)
        |                                             |
        | HTTP traffic                                v
        v                                        Aggregator
   Target under test                                  |
                                                        v
                                              VictoriaMetrics
                                                        |
                                                  Grafana / REST API
```

See [`docs/architecture.md`](docs/architecture.md) for the full
component breakdown, [`docs/lifecycle.md`](docs/lifecycle.md) for the
test/worker/assignment state machines, and
[`docs/autoscaling.md`](docs/autoscaling.md) for how fleet capacity
decisions are made.

## Tech stack

| Layer | Technology |
|---|---|
| Language | Go 1.25 |
| HTTP router | [chi](https://github.com/go-chi/chi) |
| Database | PostgreSQL 17 (via [pgx](https://github.com/jackc/pgx)) |
| Migrations | [goose](https://github.com/pressly/goose) |
| Messaging | [NATS](https://nats.io) |
| Metrics storage | [VictoriaMetrics](https://victoriametrics.com) |
| Dashboards | [Grafana](https://grafana.com) |
| Deployment | Docker Compose |

## Quick start

Requirements: Docker and Docker Compose. No local Go toolchain needed —
everything builds inside containers.

```bash
git clone <your-fork-url> Vulcan
cd Vulcan

cp .env.example .env
cp deployments/docker/.env.example deployments/docker/.env   # if present

cd deployments/docker
docker compose up -d --build
```

Wait for everything to report healthy:

```bash
docker compose ps
```

Then check the control plane is up:

```bash
curl -i http://localhost:8080/health
```

Full walkthrough — including running a load test end to end, viewing
results, and opening Grafana — is in
[`docs/deployment.md`](docs/deployment.md).

## Sample API calls

Create a test:

```bash
curl -s -X POST http://localhost:8080/api/v1/tests \
  -H "Content-Type: application/json" \
  -d '{
        "name": "smoke-test",
        "target_url": "http://host.docker.internal:9090",
        "method": "GET",
        "duration_sec": 30,
        "rps": 50,
        "concurrency": 10
      }' | jq
```

Start it:

```bash
curl -s -X POST http://localhost:8080/api/v1/tests/<test_id>/start | jq
```

Watch live metrics while it runs:

```bash
curl -s http://localhost:8080/api/v1/tests/<test_id>/metrics | jq
```

Stop it early if needed:

```bash
curl -s -X POST http://localhost:8080/api/v1/tests/<test_id>/stop
```

Full endpoint reference is in [`docs/architecture.md#api-surface`](docs/architecture.md#api-surface).
<img width="1193" height="472" alt="image" src="https://github.com/user-attachments/assets/47f1b09c-3bd4-498a-8354-147d7ade04f6" />

<img width="1916" height="476" alt="image" src="https://github.com/user-attachments/assets/1c57c08d-eea8-49b7-b6d2-54af62a71499" />

<img width="1601" height="577" alt="image" src="https://github.com/user-attachments/assets/b7833c6a-4528-4d6f-952a-520cdd7bcacf" />

## Concurrency & design decisions worth knowing

- **Test start is a single atomic transition** (`CREATED -> STARTING` via a
  conditional `UPDATE`), so two concurrent "start" calls for the same test
  can't both allocate workers.
- **Autoscaling and on-demand provisioning share one reconciler**, guarded
  by a mutex, so the periodic autoscaler loop and a test's own
  `ensureWorkersAvailable` path can never double-provision.
- **Heartbeats can't clobber authoritative state**: a worker's own
  heartbeat only ever overwrites `IDLE`/`RUNNING` with `IDLE`/`RUNNING` —
  it never overwrites `RESERVED`, `DRAINING`, or `OFFLINE`, which belong to
  the scheduler/reconciler.
- **Pending (not-yet-started) tests protect their future workers**: the
  autoscaler's scale-down floor accounts for `CREATED` tests too, so a test
  can't have its only available worker drained out from under it before it
  ever starts. See [`docs/autoscaling.md`](docs/autoscaling.md) for the
  full story, including the bug this fixed.

## Documentation

| Doc | Covers |
|---|---|
| [`docs/index.md`](docs/index.md) | Index of every phase document (0–10) |
| [`docs/architecture.md`](docs/architecture.md) | Components, data flow, API surface |
| [`docs/lifecycle.md`](docs/lifecycle.md) | Test / worker / assignment state machines |
| [`docs/autoscaling.md`](docs/autoscaling.md) | Fleet capacity decisions, concurrency safety |
| [`docs/deployment.md`](docs/deployment.md) | Docker Compose deployment, running a demo test, Grafana |

## Running tests

```bash
go test ./...
go test -race ./...
```

## Project status

Vulcan is a completed learning/portfolio project spanning Phases 0–10. The
implementation is frozen; further changes are documentation, cleanup, and
presentation polish rather than new architecture.
