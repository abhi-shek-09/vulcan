# Phase 10 — Docker Deployment & Lifecycle Hardening

## Overview

Phase 10 containerizes Vulcan (control plane, aggregator, worker, and their
infrastructure dependencies) with Docker Compose, and fixes a critical
end-to-end lifecycle bug uncovered during that containerization: a load
test could be created successfully but never leave `CREATED`, because the
Phase 9 autoscaler could drain the only available worker before the test
was ever started.

This document covers the containerized architecture, networking,
persistence, configuration, how to run the stack, how the Phase 10 bug was
diagnosed and fixed, and known limitations.

---

## Architecture

```
Host target server
       |
Control Plane (server) ---- PostgreSQL
       |                \
       |                 NATS
       v                  |
Worker (process) --------/
       |
       v (telemetry over NATS)
Aggregator ---- VictoriaMetrics ---- Grafana
```

All application services (`server`, `aggregator`, `worker`, and the `goose`
migration tool) are built from a single multi-binary image
(`deployments/docker/Dockerfile`), matching the existing Phase 7-9
single-binary-set design. There is no per-service Dockerfile because
nothing about the four binaries requires isolated build environments.

---

## Services

| Service          | Image                              | Role |
|-------------------|-------------------------------------|------|
| `postgres`         | `postgres:17`                      | Stores tests, workers, assignments |
| `nats`             | `nats:2.11-alpine`                 | Transport for worker telemetry |
| `victoriametrics`  | `victoriametrics/victoria-metrics` | Time-series metrics store |
| `grafana`          | `grafana/grafana`                  | Dashboards over VictoriaMetrics |
| `migrate`          | built from `Dockerfile`, runs `goose ... up`, then exits | Applies DB migrations before anything else starts |
| `control-plane`    | built from `Dockerfile`, `command: server` | HTTP API, scheduler, autoscaler |
| `worker`           | built from `Dockerfile`, `command: worker` | Executes load-test HTTP traffic |
| `aggregator`       | built from `Dockerfile`, `command: aggregator` | Consumes telemetry from NATS, writes to VictoriaMetrics |

The `worker` service in `docker-compose.yml` is a **static, always-on**
worker (`WORKER_HOSTNAME=worker-compose-1`) started directly by Compose --
it is not spawned by the control plane's `ProcessProvisioner`. It exists so
a freshly-started stack has at least one worker to run tests against
without needing the autoscaler to provision one first. Additional workers,
if the autoscaler decides more capacity is needed, are launched as
*processes inside the `control-plane` container* by `ProcessProvisioner`
(see [Autoscaling](#autoscaling) and [Known Limitations](#known-limitations)).

---

## Networking

Inside Compose, every service is reachable by its **service name**, which
Docker's embedded DNS resolves to the container's address on the Compose
network:

```
postgres        -> postgres:5432
nats            -> nats:4222
victoriametrics -> victoriametrics:8428
control-plane   -> control-plane:8080
```

`localhost`/`127.0.0.1` inside a container refers to that container only,
never another service -- so none of the inter-service URLs baked into
`docker-compose.yml`'s `environment:` blocks use them. `localhost` is
still correct, and used, for:

- Host-facing developer commands (`curl http://localhost:8080/health`),
  since ports are published to the host via `ports:`.
- A worker's **test target**, when the thing being load-tested runs on the
  host rather than in Compose, via Docker's special
  `host.docker.internal` DNS name (e.g.
  `"target_url": "http://host.docker.internal:9090"`).

---

## Persistent Data

Three named volumes survive `docker compose down` (but not `down -v`):

| Volume           | Mounted at                      | Contains |
|-------------------|----------------------------------|----------|
| `postgres-data`   | `/var/lib/postgresql/data` (postgres) | Tests, workers, assignments |
| `victoria-data`   | `/storage` (victoriametrics)     | Time-series telemetry |
| `grafana-data`    | `/var/lib/grafana` (grafana)     | Dashboards, users, settings |

Restarting or recreating containers (`docker compose restart`, or
`down` + `up -d` without `-v`) does not lose this data.

---

## Configuration

Vulcan's existing environment-variable-driven configuration
(`internal/config`) is unchanged for Phase 10 -- no new configuration
system was introduced. Two example files exist for the two deployment
modes:

- **`.env.example`** (repo root): running services directly on the host.
- **`deployments/docker/.env.example`**: running via Docker Compose.
  `POSTGRES_*` are substituted into `docker-compose.yml` directly; every
  other variable is an optional override of the Compose service-DNS
  defaults already baked into `docker-compose.yml` (`${VAR:-default}`
  syntax) -- you only need to set them to point a container at something
  other than the bundled Compose services.

| Variable | Used by | Purpose |
|---|---|---|
| `PORT` | control plane | HTTP listen port |
| `DB_URL` | control plane | PostgreSQL connection string |
| `NATS_URL` | control plane, worker, aggregator | NATS connection URL |
| `VICTORIA_METRICS_URL` | control plane, aggregator | VictoriaMetrics HTTP endpoint |
| `WORKER_CAPACITY_RPS` | control plane | RPS one worker is assumed to sustain; drives worker-count and autoscaler capacity math |
| `WORKER_BINARY_PATH` | control plane | Path to the worker binary `ProcessProvisioner` launches |
| `VULCAN_PROJECT_ROOT` | control plane | Working directory for provisioned worker processes (see note below) |
| `CONTROL_PLANE_URL` | control plane | Base URL advertised to newly-provisioned workers |
| `AUTOSCALER_INTERVAL` | control plane | How often the fleet reconciler runs (default `5s`) |
| `POSTGRES_DB` / `POSTGRES_USER` / `POSTGRES_PASSWORD` | postgres, migrate, control-plane | Database credentials |

> **Naming note:** the task calls this variable `WORKER_WORKING_DIR` in
> spirit, but the existing code (`internal/config/config.go`) reads it as
> `VULCAN_PROJECT_ROOT`. This was preserved as-is rather than renamed, per
> "do not introduce an entirely new configuration system" -- renaming it
> would be a gratuitous config-surface change with no functional benefit.

Neither example file contains a real secret; `deployments/docker/.env.example`
ships a placeholder `changeme` password.

---

## Running Vulcan

```bash
cd deployments/docker

# Build all images
docker compose build

# Start everything in the background
docker compose up -d

# Check status
docker compose ps

# Tail logs (all services, or a specific one)
docker compose logs -f
docker compose logs -f control-plane

# Tear down (keeps named volumes)
docker compose down

# Tear down and wipe all persistent data
docker compose down -v
```

Health endpoints once the stack is up:

```bash
curl -i http://localhost:8080/health          # control plane
curl -i http://localhost:8428/health          # VictoriaMetrics
curl -i http://localhost:3000/api/health      # Grafana
```

---

## E2E Testing

`scripts/test_phase10.sh` drives a full create -> start -> run ->
complete -> telemetry cycle against a running Compose stack, plus the
pending-test/autoscaler regression check for the bug this phase fixed, plus
a restart/persistence check.

```bash
# Bring up the stack first
cd deployments/docker && docker compose up -d && cd ../..

# Run a local HTTP target for the load test to hit
go run ./scripts/http200test &   # listens on :9090

# Run the Phase 10 script
TARGET_URL=http://host.docker.internal:9090 ./scripts/test_phase10.sh
```

---

## Autoscaling

Phase 9's autoscaler (`internal/autoscaler`) is untouched in spirit: it
still periodically reconciles the worker fleet against the capacity
required by active tests, provisions shortfalls, and drains genuinely
excess idle workers. Phase 10 fixes a gap in the **scale-down** side of
that logic (see [Root Cause & Fix](#root-cause--fix) below) so it
coexists correctly with the container lifecycle: a test that has been
created but not yet started must not have its worker pulled out from
under it.

Interaction with the containers specifically:

- The Compose `worker` service is a fixed baseline worker, always present
  as long as its container is running. It is exempt from *disappearing*
  (Compose will restart the container on failure per
  `restart: on-failure:5`), but it is **not** exempt from being marked
  `DRAINING` by the reconciler if it is genuinely idle with zero fleet
  demand -- that is correct Phase 9 behavior (Case A below), it's just no
  longer incorrectly triggered by a test sitting in `CREATED`.
- Extra workers the autoscaler decides to provision are launched as
  `worker` processes *inside the `control-plane` container* via
  `ProcessProvisioner`, per the existing Phase 9 design. No
  Docker-in-Docker, no container orchestration was introduced.

### Verified cases

- **Case A (no load):** with no pending/running tests, idle workers may
  still be scaled down -- unchanged.
- **Case B (pending test):** a `CREATED` test's required worker count now
  raises the scale-down floor, so idle capacity it needs survives until
  the test is either started or stopped. See `test_phase10.sh`'s "Pending
  test does not lose its worker" check.
- **Case C (larger load):** starting a test that needs more workers than
  are currently idle still triggers `TestService.ensureWorkersAvailable`,
  which calls into the same reconciler to provision the shortfall --
  unchanged from Phase 9.
- **Case D (active test):** `RESERVED`/`RUNNING` workers are already
  excluded from the idle/drain candidate set -- unchanged, still covered
  by `TestReconcileProtectsActiveWorkers`.
- **Case E (completion):** once a test completes and its workers return to
  `IDLE` with no other demand, they become eligible for scale-down again
  on the next reconciliation cycle -- unchanged.

---

## Root Cause & Fix

**Symptom:** a test created via `POST /api/v1/tests` stayed in `CREATED`
indefinitely. The worker polled `/api/v1/workers/{id}/assignment`
repeatedly and always got `200` with no assignment. The control plane log
showed:

```
worker fleet reconciliation desired=0 actual=1
terminating excess workers count=1
```

**Root cause:** `autoscaler.DesiredCapacity` (used by the fleet reconciler
to decide both scale-up and scale-down) only counts tests in `STARTING`
or `RUNNING` as needing capacity. A test in `CREATED` -- which has already
had a `worker_count` calculated for it, but hasn't reserved any workers
yet via `POST /tests/{id}/start` -- contributed **zero** to desired
capacity. With no active test yet, the single Compose worker sitting
`IDLE` looked like pure excess (`desired=0, actual=1`), so the periodic
reconciler drained it (`IDLE -> DRAINING`) before the test could ever be
started and reserve it. Because the Compose worker was never spawned by
`ProcessProvisioner` (it's a static Compose service, not a
provisioner-managed process), `Terminate()` had no PID to kill, silently
treated it as "already gone", and returned success -- so the worker kept
running and heartbeating, but its database row stayed `DRAINING` and
permanently ineligible for `ReserveWorkersForTest` (which only selects
`status = 'IDLE'`). The test was stranded with no worker it could ever
claim.

**Fix:** introduced `autoscaler.PendingCapacity`, which sums the worker
requirement of `CREATED` tests, kept deliberately separate from
`DesiredCapacity`:

- `DesiredCapacity` (active tests only) still drives **scale-up** --
  unchanged, a `CREATED` test does not itself trigger new provisioning
  ahead of time (that still happens on-demand inside `StartTest` via
  `ensureWorkersAvailable`, exactly as in Phase 9).
- The reconciler now computes `protectedFloor := desired + pending` and
  only drains idle capacity beyond that floor. A pending test's required
  workers survive until the test is started (at which point they're
  protected by `DesiredCapacity` itself, being `STARTING`/`RUNNING`) or
  explicitly stopped.

As a secondary hardening fix (same root failure mode, different angle): a
worker's heartbeat previously overwrote its status in the database
unconditionally with whatever the worker itself last believed
(`IDLE`/`RUNNING`) -- which meant a heartbeat could silently *un-drain* a
worker the reconciler had just marked `DRAINING`, or *un-reserve* a worker
the scheduler had just reserved for a test, purely because the worker's
own local state hadn't caught up yet. `UpdateHeartbeat` now leaves
`RESERVED`, `DRAINING`, and `OFFLINE` untouched (still refreshing
`last_heartbeat` for liveness tracking) and only lets a heartbeat write
`IDLE`/`RUNNING` over another `IDLE`/`RUNNING` value -- states the worker
actually owns.

None of this touches `ProcessProvisioner`, the scheduler, worker
assignment, or the API surface. `DesiredCapacity`'s existing behavior for
active tests is unchanged, so all pre-existing Phase 9 autoscaler tests
still pass as-is.

---

## Known Limitations

- **`ProcessProvisioner` is host/container-local, not a real orchestrator.**
  Additional workers are `os/exec` child processes of the `control-plane`
  container. They do not survive that container being killed or
  restarted, there's no cross-host provisioning, and the "fleet" is
  bounded by the resources of one container. This is unchanged from Phase
  9 by design -- Phase 10 explicitly does not introduce Docker-in-Docker
  or a container-orchestration replacement for it.
- **The Compose `worker` service is not provisioner-managed.** It's a
  convenience baseline worker started directly by Compose. If it's ever
  genuinely idle with zero fleet demand, Phase 9's scale-down policy will
  still mark it `DRAINING` (correct behavior for an autoscaled fleet) even
  though `ProcessProvisioner` can't actually terminate its process --
  it simply stays `DRAINING` (and thus unavailable) until the container is
  restarted. This is a pre-existing Phase 9/10 interaction, not something
  Phase 10 attempts to redesign; in practice it's avoided by not leaving
  the fleet at zero desired capacity for the baseline worker's idle
  lifetime, but a determined zero-load period will still drain it.
- **Local/single-host deployment only.** There is no TLS, no multi-node
  Postgres/NATS, and Grafana/VictoriaMetrics have no auth hardening beyond
  their defaults -- this Compose setup is for local development and
  demonstration, not production.
- **No automated CI wiring.** `scripts/test_phase10.sh` is meant to be run
  manually (or from a developer's CI job) against a stack already brought
  up with `docker compose up -d`; it does not itself manage the compose
  lifecycle beyond a restart check.
