# Deployment

Vulcan ships as a Docker Compose stack. This doc covers standing it up,
running a load test end to end, and where to look at the results. For the
containerized architecture, networking, volumes, and the autoscaler's
interaction with Compose specifically, see [`phase10.md`](phase10.md) —
this doc is the shorter, task-oriented version of the same material.

## Prerequisites

- Docker and Docker Compose (v2 `docker compose` syntax).
- Nothing else — the Go toolchain, Postgres client, etc. all run inside
  containers.

## 1. Clone and configure

```bash
git clone <your-fork-url> Vulcan
cd Vulcan

cp .env.example .env
cp deployments/docker/.env.example deployments/docker/.env
```

`deployments/docker/.env` only needs real values for `POSTGRES_DB`,
`POSTGRES_USER`, and `POSTGRES_PASSWORD` — everything else has a working
default baked into `docker-compose.yml` that points services at each
other by Compose service name.

## 2. Build and start

```bash
cd deployments/docker
docker compose up -d --build
```

This starts, in dependency order: `postgres` → `migrate` (runs `goose ...
up`, then exits) → `nats` / `victoriametrics` → `control-plane` →
`worker` / `aggregator` → `grafana`.

## 3. Check health

```bash
docker compose ps                                # all services healthy?

curl -i http://localhost:8080/health              # control plane
curl -i http://localhost:8428/health              # VictoriaMetrics
curl -i http://localhost:3000/api/health          # Grafana
```

## 4. Start a target to test against

Vulcan needs something to send traffic to. The repo includes a trivial
HTTP target for exactly this:

```bash
go run ./scripts/http200test &   # listens on :9090, always returns 200
```

Because the `worker` container can't reach `localhost:9090` on your host
directly, address it via Docker's host gateway:

```
http://host.docker.internal:9090
```

(If your target is itself running in Compose, use its service name
instead — see [`phase10.md#networking`](phase10.md#networking).)

## 5. Create and run a test

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

# copy the "id" from the response, then:
curl -s -X POST http://localhost:8080/api/v1/tests/<test_id>/start | jq
```

Watch it run:

```bash
docker compose logs -f worker aggregator
```

Stop it early if you need to:

```bash
curl -s -X POST http://localhost:8080/api/v1/tests/<test_id>/stop
```

## 6. View results

**Live snapshot** (from the control plane, backed by VictoriaMetrics):

```bash
curl -s http://localhost:8080/api/v1/tests/<test_id>/metrics | jq
```

**Historical series** over a time range:

```bash
curl -s "http://localhost:8080/api/v1/tests/<test_id>/metrics/history?start=-5m&end=now&step=5s" | jq
```

**Inspect VictoriaMetrics directly** (its own HTTP query API):

```bash
curl -s "http://localhost:8428/api/v1/query?query=vulcan_requests_total" | jq
```

**Open Grafana**: [http://localhost:3000](http://localhost:3000)
(default credentials `admin` / `admin` unless overridden). The Vulcan
dashboard is provisioned automatically from
`deployments/grafana/dashboards/vulcan.json` against the VictoriaMetrics
datasource in `deployments/grafana/provisioning/`.

## 7. Watch workers scale

If you start a test whose RPS needs more workers than are currently idle,
the autoscaler provisions the shortfall automatically — no manual action
needed. Watch it happen:

```bash
docker compose logs -f control-plane | grep -i "worker fleet"
curl -s http://localhost:8080/api/v1/workers | jq
```

You should see additional workers with hostnames like
`vulcan-auto-worker-<timestamp>-<n>` appear, move through
`RESERVED`/`RUNNING`, and return to `IDLE` once the test completes. See
[`autoscaling.md`](autoscaling.md) for exactly how that decision is made.

## 8. Tear down

```bash
docker compose down        # stop everything, keep volumes (Postgres/VM/Grafana data)
docker compose down -v     # stop everything and wipe all persisted data
```

## End-to-end test script

`scripts/test_phase10.sh` automates steps 4–7 above (create → start → run
→ complete → verify telemetry), plus the pending-test/autoscaler
regression check and a restart/persistence check:

```bash
cd deployments/docker && docker compose up -d && cd ../..
go run ./scripts/http200test &
TARGET_URL=http://host.docker.internal:9090 ./scripts/test_phase10.sh
```

## Environment variables

| Variable | Used by | Purpose |
|---|---|---|
| `PORT` | control plane | HTTP listen port |
| `DB_URL` | control plane | PostgreSQL connection string |
| `NATS_URL` | control plane, worker, aggregator | NATS connection URL |
| `VICTORIA_METRICS_URL` | control plane, aggregator | VictoriaMetrics HTTP endpoint |
| `WORKER_CAPACITY_RPS` | control plane | RPS one worker is assumed to sustain |
| `WORKER_BINARY_PATH` | control plane | Path to the worker binary the autoscaler launches |
| `VULCAN_PROJECT_ROOT` | control plane | Working directory for autoscaler-provisioned worker processes |
| `CONTROL_PLANE_URL` | control plane, worker | Base URL workers register/poll against |
| `AUTOSCALER_INTERVAL` | control plane | How often the fleet reconciler runs (default `5s`) |
| `POSTGRES_DB` / `POSTGRES_USER` / `POSTGRES_PASSWORD` | postgres, migrate, control-plane | Database credentials |

## Known limitations of this deployment

- **Single-host only.** `ProcessProvisioner` launches worker processes as
  children of the `control-plane` container — there's no cross-host
  provisioning and no Docker-in-Docker. The fleet is bounded by that one
  container's resources.
- **No TLS, no auth hardening.** Grafana/VictoriaMetrics use their
  defaults; this is a local/demo setup, not a production one.
- **The Compose baseline worker isn't provisioner-managed** and is exempt
  from scale-down eligibility for that reason — see
  [`autoscaling.md#known-limitation-the-compose-baseline-worker`](autoscaling.md#known-limitation-the-compose-baseline-worker).

See [`phase10.md`](phase10.md) for the full diagnosis of the lifecycle bug
this containerization phase fixed, if you want the complete history.
