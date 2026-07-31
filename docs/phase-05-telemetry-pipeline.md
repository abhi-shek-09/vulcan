# Vulcan Phase 5
# Distributed Telemetry Pipeline

Author: Abhishek Murthy

---

# Overview

Phase 5 introduces Vulcan's distributed telemetry pipeline.

Previous phases focused on orchestration:

- Worker registration
- Scheduling
- Assignment lifecycle
- Heartbeats
- Distributed execution

At the end of Phase 4, workers could execute assigned tests but did not produce meaningful telemetry.

Execution consisted only of a simulated sleep.

```
Assignment

↓

Sleep(10s)

↓

Complete
```

This phase replaces that model with a distributed metrics pipeline.

Workers now simulate request execution, aggregate metrics locally, publish telemetry to NATS, where a dedicated Aggregator service consumes and merges metrics before storing them inside VictoriaMetrics.

The Control Plane continues to remain orchestration-only.

It never processes high-frequency metrics.

---

# Objectives

The objectives of this phase were:

- Introduce worker-side telemetry collection
- Aggregate metrics locally
- Publish aggregated buckets instead of individual requests
- Introduce NATS messaging
- Build a Metrics Aggregator service
- Aggregate metrics globally
- Persist metrics inside VictoriaMetrics
- Verify end-to-end telemetry flow

---

# Final Architecture

```
                     +----------------------+
                     |   Control Plane      |
                     +----------+-----------+
                                |
                        Scheduler / Assignments
                                |
       --------------------------------------------------
       |                     |                         |
       ▼                     ▼                         ▼

   Worker Runtime      Worker Runtime           Worker Runtime

       │                     │                         │
       ▼                     ▼                         ▼

 Local Collector      Local Collector         Local Collector

       │                     │                         │
       └────────────── Publish MetricBucket ───────────┘
                              │
                              ▼
                            NATS
                              │
                              ▼
                    Metrics Aggregator
                              │
                              ▼
                     VictoriaMetrics
                              │
                              ▼
                           Grafana
```

---

# Why NATS?

If every worker wrote directly into PostgreSQL:

```
100 Workers

↓

100 Inserts / second

↓

Database Bottleneck
```

The Control Plane would become responsible for processing telemetry instead of orchestration.

Instead:

```
Worker

↓

Aggregate

↓

Publish

↓

Aggregator

↓

VictoriaMetrics
```

Benefits:

- loose coupling
- asynchronous processing
- scalable
- workers never block on database writes
- Control Plane remains lightweight

---

# Worker Execution Flow

Worker execution is now:

```
Receive Assignment

↓

Generate Simulated Requests

↓

Measure

• Latency
• Success
• Failure
• Bytes Sent
• Bytes Received

↓

Collector

↓

Aggregate 1-second bucket

↓

Publish MetricBucket

↓

Repeat

↓

Complete Assignment
```

The worker publishes one MetricBucket every second.

No individual request is transmitted.

---

# MetricBucket

The telemetry pipeline revolves around MetricBucket.

```go
type MetricBucket struct {
    TestID string
    WorkerID string

    WindowStart time.Time
    WindowEnd time.Time

    Requests int64
    Successes int64
    Failures int64

    BytesSent int64
    BytesReceived int64

    AvgLatencyMs float64
    MaxLatencyMs float64
}
```

Each bucket represents one aggregation window.

The bucket contains summarized telemetry instead of raw request events.

---

# Why Aggregate Locally?

Suppose one worker generates:

```
100 requests/sec
```

Sending every request individually would produce:

```
100 messages/sec
```

Across 500 workers:

```
50,000 messages/sec
```

Instead the worker aggregates locally:

```
100 requests

↓

1 MetricBucket

↓

1 NATS Message
```

Result:

Massive reduction in network traffic.

---

# Collector

Each executing worker owns a Collector.

Responsibilities:

- count requests
- count successes
- count failures
- sum bytes sent
- sum bytes received
- accumulate latency
- compute average latency
- compute maximum latency

Every second:

```
Collector

↓

Snapshot()

↓

MetricBucket

↓

Reset()

↓

Continue
```

---

# Simulated Request Generation

Actual HTTP load generation is intentionally deferred to Phase 6.

For this phase each worker generates simulated requests.

Each request randomly produces:

- latency
- success/failure
- bytes sent
- bytes received

The Collector receives these values exactly as it will in Phase 6.

Only the source of telemetry changes later.

---

# NATS

Workers publish to:

```
metrics.test.<testID>
```

Example:

```
metrics.test.01KYWSAYJMA0E788Z12XHNJMCG
```

The Aggregator subscribes using:

```
metrics.test.*
```

This allows one Aggregator to process telemetry from every active test.

---

# Publisher

The worker owns a Publisher responsible for:

```
MetricBucket

↓

JSON Marshal

↓

NATS Publish
```

Workers are unaware of aggregation logic.

They simply publish buckets.

---

# Metrics Aggregator

A dedicated Aggregator service subscribes to NATS.

Responsibilities:

- receive MetricBuckets
- merge buckets
- compute global metrics
- write to VictoriaMetrics

It has no HTTP API.

It communicates only through NATS.

---

# Global Aggregation

The Aggregator merges telemetry from every worker participating in a test.

Example:

Worker A

```
Requests = 100
```

Worker B

```
Requests = 100
```

Worker C

```
Requests = 100
```

Merged result:

```
Requests = 300
```

Likewise:

- Successes
- Failures
- Bytes
- Latency

are all merged into a global view.

---

# Snapshot-Based Reporting

The Aggregator maintains mutable aggregation state internally.

Every reporting interval:

1. Metrics are copied into a snapshot.
2. Aggregation state is reset.
3. Locks are released.
4. Logging and VictoriaMetrics writes occur outside the critical section.

Advantages:

- minimal lock contention
- workers continue publishing while metrics are written
- scalable with larger worker counts

This avoids blocking incoming telemetry during network I/O.

---

# VictoriaMetrics Integration

The Metrics Aggregator persists aggregated telemetry inside VictoriaMetrics.

VictoriaMetrics was chosen because:

- Prometheus-compatible API
- Extremely high ingestion rate
- Low memory usage
- Native Grafana integration
- Suitable for high-cardinality telemetry

Unlike PostgreSQL, VictoriaMetrics is designed specifically for time-series data.

---

# Metrics Written

The Aggregator currently writes the following metrics.

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

Each metric is tagged with:

```
test_id
```

Example:

```
vulcan_requests_total{test_id="01KYWSAYJMA0E788Z12XHNJMCG"} 300
```

This allows metrics belonging to multiple concurrent load tests to coexist safely.

---

# End-to-End Execution Flow

Complete telemetry pipeline:

```
Create Test

↓

Reserve Workers

↓

Start Test

↓

Workers Receive Assignment

↓

Worker Executes

↓

Collector Aggregates

↓

MetricBucket

↓

Publish to NATS

↓

Aggregator Receives Bucket

↓

Merge Global State

↓

Compute Global Metrics

↓

Write to VictoriaMetrics

↓

Grafana / Query API
```

The Control Plane is never involved after assigning work.

---

# Manual Testing Procedure

## Step 1

Start PostgreSQL.

---

## Step 2

Start NATS.

Example:

```
docker run -p 4222:4222 nats
```

---

## Step 3

Start VictoriaMetrics.

```
docker run \
-p 8428:8428 \
victoriametrics/victoria-metrics
```

---

## Step 4

Start Control Plane.

```
make run-server
```

---

## Step 5

Start Aggregator.

```
make run-aggregator
```

Expected:

```
Aggregator connected to NATS.
```

---

## Step 6

Start three workers.

Example:

Terminal 1

```
WORKER_HOSTNAME=worker-1 make run-worker
```

Terminal 2

```
WORKER_HOSTNAME=worker-2 make run-worker
```

Terminal 3

```
WORKER_HOSTNAME=worker-3 make run-worker
```

Workers should register successfully.

---

## Step 7

Create a test.

Example:

```
POST /api/v1/tests
```

Payload:

```json
{
    "name":"Telemetry Test",
    "worker_count":3
}
```

Record the returned test ID.

---

## Step 8

Start the test.

```
POST /api/v1/tests/{id}/start
```

Workers should transition:

```
IDLE

↓

RUNNING
```

---

## Step 9

Observe Worker Logs

Workers should continuously log:

```
metric bucket generated

↓

metric bucket published
```

Example:

```
requests=100

successes=97

failures=3
```

---

## Step 10

Observe Aggregator Logs

Example:

```
global metrics

workers=3

requests=300

success_rate=96%

avg_latency_ms=104

bytes_sent=301122

bytes_received=901553
```

---

## Step 11

Verify VictoriaMetrics

Run:

```
curl \
"http://localhost:8428/api/v1/query?query=vulcan_requests_total"
```

Expected:

```json
{
  "status":"success",
  "data":{
      ...
  }
}
```

This confirms successful persistence.

---

# Expected Behaviour

Three workers should produce approximately:

```
Worker 1

100 req/s

+

Worker 2

100 req/s

+

Worker 3

100 req/s

↓

Global

≈300 req/s
```

Minor variations are expected because reporting windows are not perfectly synchronized.

---

# Locking Strategy

The Aggregator minimizes contention by:

```
Lock

↓

Copy Metrics

↓

Reset Window

↓

Unlock

↓

Logger

↓

VictoriaMetrics Write
```

HTTP requests are intentionally performed outside critical sections.

This allows workers to continue publishing while metrics are being persisted.

---

# Design Decisions

## Workers Never Write to PostgreSQL

Reason:

Telemetry volume is significantly higher than orchestration traffic.

Keeping telemetry out of PostgreSQL prevents database contention.

---

## Bucket-Based Telemetry

Workers publish one summary every second.

Advantages:

- Reduced network traffic
- Lower CPU usage
- Lower serialization overhead
- Easier aggregation

---

## NATS Instead of Direct Calls

NATS provides:

- asynchronous messaging
- loose coupling
- scalability
- fault isolation

Workers remain unaware of downstream consumers.

---

## Dedicated Aggregator

Aggregation is isolated from the Control Plane.

Advantages:

- independent scaling
- independent deployment
- simpler Control Plane
- future horizontal scaling

---

## VictoriaMetrics

Chosen because it is purpose-built for time-series storage.

Advantages:

- efficient ingestion
- Prometheus compatibility
- Grafana integration
- low operational complexity

---

# Known Limitations

Current implementation intentionally simulates request execution.

Workers do not yet:

- execute real HTTP requests
- support configurable request rates
- measure network latency
- support request payloads
- reuse HTTP clients
- handle retries

These features are introduced in Phase 6.

---

# Future Work

Phase 6 replaces simulated requests with a real HTTP engine.

Instead of generating random metrics:

```
Simulated Request

↓

Collector
```

Workers will execute:

```
HTTP Request

↓

Measure

↓

Collector
```

The telemetry pipeline itself remains unchanged.

Only the data source changes.

This separation was an intentional design goal of Phase 5.

---

# Phase Summary

Phase 5 successfully introduces Vulcan's distributed telemetry pipeline.

Implemented components include:

- Worker-side telemetry collection
- Local metric aggregation
- MetricBucket model
- NATS publisher
- NATS subscriber
- Metrics Aggregator
- Global aggregation
- VictoriaMetrics integration
- Multi-worker telemetry validation
- End-to-end verification

The project now supports distributed metric collection and storage while keeping the Control Plane focused exclusively on orchestration.

This telemetry architecture becomes the foundation for real HTTP load generation, dashboards, and production-scale deployments in subsequent phases.