# Vulcan Phase 5
# Distributed Telemetry Pipeline (Updated)

**Author:** Abhishek Murthy

---

# Overview

Phase 5 transforms Vulcan from a distributed orchestration platform into a distributed telemetry platform.

Previous phases established the foundation required to execute distributed load tests:

- Worker registration
- Worker heartbeats
- Distributed scheduling
- Worker reservation
- Assignment lifecycle
- Test orchestration

At the conclusion of Phase 4, workers were capable of receiving assignments and executing them, but execution was only simulated. No meaningful telemetry was produced, aggregated or persisted.

Phase 5 introduces Vulcan's complete telemetry pipeline.

Instead of workers simply completing assigned jobs, every worker now executes a configurable load generation loop, continuously produces execution metrics, aggregates them locally, and publishes summarized telemetry through NATS. A dedicated Metrics Aggregator consumes telemetry from every worker, computes global statistics, and persists those metrics into VictoriaMetrics for long-term storage and visualization.

The Control Plane intentionally remains responsible only for orchestration.

It never processes high-frequency metrics and never becomes part of the telemetry data path.

---

# Objectives

The primary objectives of Phase 5 were:

- Introduce configurable load test definitions
- Extend the Test model with execution parameters
- Build a worker-side execution engine
- Collect execution metrics locally
- Aggregate metrics into one-second buckets
- Publish MetricBuckets through NATS
- Build a dedicated Metrics Aggregator
- Merge telemetry across all participating workers
- Persist aggregated metrics into VictoriaMetrics
- Validate the complete telemetry pipeline end-to-end

---

# Phase 5 Architecture

```text
                         +----------------------+
                         |    Control Plane     |
                         +----------+-----------+
                                    |
                          Scheduler / Assignments
                                    |
      ---------------------------------------------------------------
      |                         |                         |
      ▼                         ▼                         ▼

  Worker Runtime          Worker Runtime          Worker Runtime

      │                         │                         │
      ▼                         ▼                         ▼

 Execution Engine        Execution Engine        Execution Engine

      │                         │                         │
      ▼                         ▼                         ▼

 Local Collector        Local Collector         Local Collector

      │                         │                         │
      └────────────── Publish MetricBucket ───────────────┘
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

The telemetry pipeline is completely independent of the Control Plane after a test begins.

Once workers receive assignments, all execution metrics flow directly from workers to the Aggregator through NATS.

---

# Separation of Responsibilities

The architecture deliberately separates orchestration from telemetry.

## Control Plane

Responsible for:

- Creating tests
- Scheduling workers
- Reserving workers
- Starting tests
- Assignment lifecycle
- Worker heartbeats
- Metadata persistence

The Control Plane does **not** process request metrics.

---

## Worker

Responsible for:

- Executing assigned load tests
- Generating request metrics
- Aggregating telemetry locally
- Publishing MetricBuckets to NATS

Workers never communicate directly with VictoriaMetrics.

---

## Metrics Aggregator

Responsible for:

- Receiving MetricBuckets
- Merging worker telemetry
- Computing global statistics
- Persisting metrics into VictoriaMetrics

The Aggregator has no knowledge of scheduling or worker assignment.

---

## VictoriaMetrics

Responsible for:

- Time-series storage
- Historical metrics
- Prometheus-compatible querying
- Grafana integration

---

# Why This Architecture?

Without a dedicated telemetry pipeline, every worker would continuously write metrics directly to the Control Plane or PostgreSQL.

For example:

```text
300 Workers

↓

100 Requests / Second

↓

30,000 Metrics Every Second

↓

Database Bottleneck
```

The Control Plane would become responsible for processing telemetry instead of coordinating distributed execution.

Instead, Vulcan follows a publish-subscribe architecture:

```text
Worker

↓

Collector

↓

MetricBucket

↓

NATS

↓

Aggregator

↓

VictoriaMetrics
```

Advantages include:

- Loose coupling
- Horizontal scalability
- Asynchronous communication
- Reduced database pressure
- Lower network overhead
- Simpler Control Plane
- Independent service scaling

---

# Configurable Load Tests

Unlike previous phases, tests are no longer simple metadata records.

Each test now contains enough information for workers to execute a configurable workload.

The Test model now includes:

```go
type Test struct {
    ID            string
    Name          string
    Status        TestStatus

    WorkerCount   int

    TargetURL     string
    Method        string

    DurationSec   int
    RPS           int
    Concurrency   int

    CreatedAt     time.Time
    UpdatedAt     time.Time
}
```

These fields describe how a worker should execute the assigned workload.

Current parameters include:

| Field | Purpose |
|--------|----------|
| TargetURL | Target endpoint |
| Method | HTTP method |
| DurationSec | Test duration |
| RPS | Desired request rate |
| Concurrency | Concurrent execution level |

Although request execution remains simulated in Phase 5, the execution engine now operates using realistic load-test parameters rather than fixed-duration sleeps.

This design allows Phase 6 to replace only the request generation component while leaving the surrounding telemetry pipeline unchanged.

---

# Worker Execution Flow

Worker execution has been redesigned.

Previous implementation:

```text
Receive Assignment

↓

Sleep(10 Seconds)

↓

Complete Assignment
```

Current implementation:

```text
Receive Assignment

↓

Load Test Configuration

↓

Start Execution Loop

↓

Generate Request Metrics

↓

Collector

↓

Aggregate One-Second Window

↓

Create MetricBucket

↓

Publish to NATS

↓

Repeat Until Duration Ends

↓

Complete Assignment
```

Execution now continues for the configured test duration.

Every execution cycle contributes telemetry to the Collector.

The worker publishes summarized telemetry every second while the assignment is active.

---

# Simulated Execution Engine

Actual HTTP requests are intentionally deferred to Phase 6.

Instead, workers generate realistic execution statistics including:

- Request count
- Successes
- Failures
- Request latency
- Maximum latency
- Bytes sent
- Bytes received

These values are generated using configurable distributions so that the telemetry pipeline can be validated without introducing network variability.

The remainder of the telemetry pipeline treats simulated metrics exactly the same as real request metrics.

This allows the Collector, Aggregator and VictoriaMetrics integration to be fully developed and validated before implementing the production HTTP engine.

---

# Local Metric Collection

Each worker owns an independent Collector.

The Collector is responsible for accumulating execution statistics over a fixed one-second reporting window.

Rather than publishing every request individually, the Collector summarizes all requests generated during the window into a single MetricBucket.

Benefits include:

- Reduced serialization overhead
- Lower network traffic
- Lower CPU utilization
- Simpler aggregation
- Better scalability

The Collector therefore acts as the boundary between high-frequency request generation and low-frequency telemetry publication.

---

# One-Second Aggregation Window

Each worker continuously performs the following cycle:

```text
Generate Request Metrics

↓

Collector Records Metrics

↓

One Second Elapses

↓

Collector Snapshot

↓

MetricBucket Created

↓

Collector Reset

↓

Continue Execution
```

The reporting window is fixed at one second.

Each published MetricBucket therefore represents an aggregate view of all requests executed during that interval rather than individual request events.

---

# MetricBucket

The MetricBucket is the fundamental telemetry unit within Vulcan.

```go
type MetricBucket struct {
    TestID string
    WorkerID string

    WindowStart time.Time
    WindowEnd   time.Time

    Requests int64

    Successes int64
    Failures  int64

    BytesSent     int64
    BytesReceived int64

    AvgLatencyMs float64
    MaxLatencyMs float64
}
```

Every bucket represents exactly one aggregation window produced by a single worker.

The Aggregator receives these buckets and merges them into global test metrics.

---

# Complete Runtime Sequence

Once all services are running, the complete telemetry pipeline works as follows.

```
                Control Plane
                      │
          Create / Start Test
                      │
                      ▼
          Worker Assignments Sent
                      │
      ┌───────────────┼───────────────┐
      ▼               ▼               ▼
   Worker 1        Worker 2        Worker 3
      │               │               │
      │ Generate HTTP Requests        │
      │               │               │
      └────── Aggregate 1-second Metrics ──────┘
                      │
                      ▼
              Publish MetricBucket
                  (NATS Subject)
                      │
                      ▼
                    NATS
                      │
                      ▼
                 Aggregator
                      │
        Merge Worker Buckets Per Test
                      │
                      ▼
          Compute Global Statistics
                      │
                      ▼
         Convert to Prometheus Format
                      │
                      ▼
            VictoriaMetrics Import API
                      │
                      ▼
             Metrics Stored Persistently
                      │
                      ▼
      Queryable by Grafana / PromQL APIs
```

---

# Step 1 — Test Creation

The Control Plane receives:

```
POST /api/v1/tests
```

Example request:

```json
{
    "name":"HTTP Test",
    "worker_count":3,
    "target_url":"https://httpbin.org/get",
    "method":"GET",
    "duration_sec":30,
    "rps":100,
    "concurrency":20
}
```

The database stores:

- metadata
- target URL
- duration
- RPS
- concurrency
- timestamps

No workers begin execution yet.

---

# Step 2 — Start Test

Calling

```
POST /api/v1/tests/{id}/start
```

causes the scheduler to:

- reserve idle workers
- assign workers
- update worker status
- update test status

The API returns

```json
{
    "test_id":"...",
    "status":"RUNNING",
    "workers":[
        {
            "id":"...",
            "hostname":"worker-1"
        }
    ]
}
```

---

# Step 3 — Worker Execution

Each worker receives an assignment.

Example:

```
Target:
https://httpbin.org/get

Method:
GET

Duration:
30 sec

RPS:
100

Concurrency:
20
```

The worker begins generating HTTP traffic.

---

# Step 4 — Local Aggregation

Workers never publish every request.

Instead they aggregate one-second windows.

Example:

```
Requests:
100

Successes:
96

Failures:
4

Average Latency:
103 ms

Max Latency:
198 ms

Bytes Sent:
100 KB

Bytes Received:
310 KB
```

These values become one `MetricBucket`.

---

# Step 5 — NATS Publish

Each second the worker publishes

```
metrics.test.<testID>
```

Payload:

```json
{
    "test_id":"...",
    "worker_id":"...",
    "requests":100,
    "successes":96,
    "failures":4,
    "avg_latency_ms":103,
    "max_latency_ms":198
}
```

Thousands of requests become a single compact telemetry message.

---

# Step 6 — Aggregator Subscription

The Aggregator subscribes to

```
metrics.test.*
```

Every incoming bucket is unmarshalled.

The Store merges buckets into one shared window.

Instead of

```
Worker 1
Worker 2
Worker 3
```

the Aggregator builds

```
Global Test Window
```

---

# Step 7 — Global Metric Computation

Every second the Aggregator computes:

```
Requests

Successes

Failures

Average Latency

Maximum Latency

Bytes Sent

Bytes Received

Worker Count

Success Rate

RPS
```

Example log:

```
global metrics

workers=3

requests=300

successes=287

failures=13

success_rate=95.67

avg_latency_ms=100.3

max_latency_ms=199
```

---

# Step 8 — Prometheus Payload Creation

The Aggregator converts snapshots into Prometheus exposition format.

Example:

```text
vulcan_requests_total{test_id="01ABC"} 300
vulcan_success_total{test_id="01ABC"} 287
vulcan_failure_total{test_id="01ABC"} 13
vulcan_workers{test_id="01ABC"} 3
vulcan_avg_latency_ms{test_id="01ABC"} 100.31
vulcan_max_latency_ms{test_id="01ABC"} 199
vulcan_bytes_sent_total{test_id="01ABC"} 304459
vulcan_bytes_received_total{test_id="01ABC"} 919694
```

---

# Step 9 — VictoriaMetrics Storage

The payload is POSTed to

```
POST /api/v1/import/prometheus
```

VictoriaMetrics parses the metrics and stores them as time series.

Each metric is tagged with

```
test_id
```

allowing multiple tests to coexist.

---

# Step 10 — Query Layer

Metrics can now be queried using the Prometheus API.

Examples:

Latest request count

```
vulcan_requests_total
```

Failures

```
vulcan_failure_total
```

Average latency

```
vulcan_avg_latency_ms
```

Workers

```
vulcan_workers
```

Historical export

```
/api/v1/export?match[]=vulcan_requests_total
```

---

# Typical Runtime Timeline

```
Time 0
Create Test

↓

Start Test

↓

Workers Assigned

↓

Workers Generate Requests

↓

Workers Aggregate Metrics

↓

Workers Publish MetricBuckets

↓

Aggregator Merges Buckets

↓

Aggregator Computes Global Snapshot

↓

VictoriaMetrics Stores Snapshot

↓

Grafana Queries Metrics
```

---

# Pipeline Characteristics

The implemented telemetry pipeline provides:

- Distributed metric collection
- One-second aggregation windows
- Minimal network overhead
- Event-driven communication using NATS
- Centralized aggregation
- Prometheus-compatible metric formatting
- Time-series persistence in VictoriaMetrics
- Query support through standard Prometheus APIs
- Separation between execution workers and storage backend
- Scalability by adding more workers without changing the aggregation model
