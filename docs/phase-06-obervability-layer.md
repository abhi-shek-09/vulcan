# Vulcan Phase 6 — Observability Layer

## Overview

Phase 6 completed the observability stack for Vulcan.

Prior phases enabled:

- Distributed worker scheduling
- Worker execution
- Metric aggregation
- Telemetry publishing through NATS

Phase 6 adds persistent storage, querying, visualization and dashboard APIs.

The completed telemetry pipeline is now:

Worker
→ NATS
→ Aggregator
→ VictoriaMetrics
→ Dashboard Service
→ REST API
→ Grafana

This phase transforms Vulcan from a distributed load generator into a fully observable distributed system.

---

# Objectives

The goals of Phase 6 were:

- Integrate VictoriaMetrics
- Integrate Grafana
- Persist aggregated metrics
- Expose metrics through REST APIs
- Support historical queries
- Provision dashboards automatically
- Remove hardcoded infrastructure addresses
- Containerize the observability stack

---

# Architecture

                   Control Plane
                         │
                         │
                Dashboard API
                         │
                         ▼
                 VictoriaMetrics
                         ▲
                         │
                  Aggregator Service
                         ▲
                         │
                       NATS
                         ▲
                         │
                    Worker Fleet

---

# Major Components

## Dashboard Package

New package:

```
internal/dashboard
```

Contains:

```
client.go
models.go
parser.go
queries.go
service.go
```

Responsibilities:

- Execute PromQL queries
- Execute range queries
- Parse VictoriaMetrics responses
- Convert responses into API models

---

## VictoriaMetrics Client

New package:

```
internal/victoria
```

Responsibilities:

- Convert snapshots into Prometheus exposition format
- POST metrics into VictoriaMetrics
- Handle ingestion failures

---

## Dashboard API

New endpoints

### Live Metrics

```
GET /api/v1/tests/{id}/metrics
```

Returns

- Requests
- Successes
- Failures
- Workers
- Average latency
- Maximum latency
- Bytes sent
- Bytes received

---

### Historical Metrics

```
GET /api/v1/tests/{id}/history
```

Supports:

- start
- end
- step

Uses VictoriaMetrics query_range.

---

# VictoriaMetrics Metrics

The Aggregator periodically writes

```
vulcan_requests_total
vulcan_success_total
vulcan_failure_total

vulcan_workers

vulcan_avg_latency_ms
vulcan_max_latency_ms

vulcan_bytes_sent_total
vulcan_bytes_received_total
```

Each metric contains

```
test_id=<ULID>
```

allowing dashboard queries per test.

---

# Grafana

Added

```
docker/grafana/
```

Datasource provisioning

Dashboard provisioning

Dashboard JSON

Grafana automatically starts with:

- datasource configured
- dashboard installed

No manual configuration required.

---

# Configuration Changes

Removed all hardcoded infrastructure URLs.

Instead:

```
internal/config
```

reads

```
VICTORIA_METRICS_URL

NATS_URL
```

from

- .env (local execution)
- docker-compose environment variables

This allows identical binaries to run locally and inside Docker.

---

# Docker Stack

Phase 6 Compose services

- Control Plane
- PostgreSQL
- NATS
- Aggregator
- VictoriaMetrics
- Grafana

Workers intentionally remain outside Docker.

Reason:

Workers represent machines that exist outside the control plane.

Running workers directly on the host more accurately simulates remote agents and makes it easier to scale by launching many worker processes.

Future Kubernetes support will replace local worker execution.

---

# Major Bug Encountered

## Symptom

Dashboard returned

Requests = 0

Workers = 0

Latency = 0

while VictoriaMetrics already contained telemetry.

---

## Investigation

Verified:

✓ Aggregator receiving NATS messages

✓ Aggregator computing snapshots

✓ VictoriaMetrics ingest succeeding

✓ Metrics visible through direct PromQL

However Dashboard API still received

```
result=[]
```

---

## Root Cause

VictoriaMetrics indexing is eventually consistent.

Immediately after ingestion, instant queries may temporarily return

```
result=[]
```

before the index becomes visible.

The dashboard queried too early.

---

## Resolution

Adjusted the integration flow to wait until telemetry became queryable before validating metrics.

After the fix:

- Dashboard API returned correct telemetry
- History endpoint returned valid series
- Grafana visualized metrics correctly

---

# Testing Procedure

Start infrastructure

```bash
docker compose up -d --build
```

Launch workers

```bash
go run cmd/worker/main.go
```

Repeat for three workers.

Execute

```bash
./scripts/test_phase6.sh
```

Expected validations:

✓ Control Plane

✓ VictoriaMetrics

✓ Grafana

✓ Test creation

✓ Test start

✓ Live metrics endpoint

✓ History endpoint

✓ VictoriaMetrics query

✓ Datasource provisioning

✓ Dashboard provisioning

Expected output

```
===============================================
PHASE 6 TESTS PASSED
===============================================
```

---

# Deliverables

Completed:

- VictoriaMetrics integration
- Grafana integration
- Dashboard API
- Historical API
- Dashboard package
- Victoria writer
- Automatic provisioning
- Configuration cleanup
- Docker observability stack

---

# Phase Status

| Phase | Status |
|--------|--------|
| Phase 0 | Complete |
| Phase 1 | Complete |
| Phase 2 | Complete |
| Phase 3 | Complete |
| Phase 4 | Complete |
| Phase 5 | Complete |
| Phase 6 | ✅ Complete |

The Vulcan platform now supports complete telemetry collection, persistence, querying and visualization.