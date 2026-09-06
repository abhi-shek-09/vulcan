# Documentation Index

Vulcan was built in ten phases, each shipped as a working increment on top
of the previous one. The phase documents below are the original build log;
the topic docs alongside them are the consolidated, final-state reference.

## Start here

| Doc | What it's for |
|---|---|
| [`architecture.md`](architecture.md) | Components, data flow, full API reference |
| [`lifecycle.md`](lifecycle.md) | Test / worker / assignment state machines |
| [`autoscaling.md`](autoscaling.md) | Fleet capacity decisions and concurrency safety |
| [`deployment.md`](deployment.md) | Docker Compose deployment, running a demo test |

## Phase-by-phase build log

| Phase | Doc | Focus |
|---|---|---|
| 1 | [`phase-01-control-plane.md`](phase-01-control-plane.md) | Control plane foundation: HTTP API, Postgres, project skeleton |
| 2 | [`phase-02-worker-management.md`](phase-02-worker-management.md) | Worker registration, heartbeats, worker CRUD |
| 3 | [`phase-03-worker-scheduling.md`](phase-03-worker-scheduling.md) | Worker scheduling and resource allocation |
| 4 | [`phase-04-worker-runtime.md`](phase-04-worker-runtime.md) | Distributed scheduling and the worker runtime loop |
| 5 | [`phase-05-telemetry-pipeline.md`](phase-05-telemetry-pipeline.md) | NATS-based telemetry pipeline from worker to aggregator |
| 6 | [`phase-06-obervability-layer.md`](phase-06-obervability-layer.md) | VictoriaMetrics + Grafana observability layer |
| 7 | [`phase-07-worker-failure-resilience.md`](phase-07-worker-failure-resilience.md) | Worker failure detection and assignment failover |
| 8 | [`phase-08-worker-fleet-orchestration.md`](phase-08-worker-fleet-orchestration.md) | Full worker-fleet lifecycle management |
| 9 | *(see [`autoscaling.md`](autoscaling.md))* | Fleet autoscaler (`internal/autoscaler`): desired/pending capacity, scale-up/down. No standalone Phase 9 doc was written at the time — the design is captured in code comments and consolidated in `autoscaling.md`. |
| 10 | [`phase10.md`](phase10.md) | Docker Compose deployment and the pending-test/autoscaler lifecycle bugfix |

## Reading order

If you're new to the codebase, read `architecture.md` → `lifecycle.md` →
`autoscaling.md` → `deployment.md` first. The phase docs are useful for
*why* a particular piece of code looks the way it does, but they were
written incrementally and some early assumptions were later revised (most
notably around Phase 9/10, where the autoscaler's scale-down logic was
corrected) — the topic docs above reflect the final, frozen implementation.
