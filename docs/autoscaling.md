# Autoscaling

The autoscaler (`internal/autoscaler`) is the single source of truth for
how many workers should exist. Everything else — `StartTest`'s on-demand
provisioning, the periodic background loop — funnels through the same
`Reconciler.Reconcile` call so fleet-sizing decisions can never be made by
two code paths at once.

## Capacity math

Two independent numbers feed into every reconciliation cycle
(`internal/autoscaler/capacity.go`):

- **`DesiredCapacity`** — sum of `RequiredWorkers(test.RPS,
  WorkerCapacityRPS)` for every test in `STARTING` or `RUNNING`. This
  drives **scale-up**: it's "how much capacity do currently active tests
  need right now."
- **`PendingCapacity`** — the same sum, but for tests still in `CREATED`.
  This exists purely to protect **scale-down**: a `CREATED` test hasn't
  reserved any workers yet (that only happens inside `StartTest`), so it
  deliberately does *not* trigger new provisioning ahead of time — but the
  idle capacity it will need must not be drained by the periodic loop
  before the test gets a chance to start.

`RequiredWorkers` is `ceil(rps / workerCapacityRPS)` — the same formula
used by the scheduler when a test is created, so the autoscaler never
disagrees with the scheduler about how many workers a given test needs.

## Reconciliation

`Reconciler.Reconcile` (`internal/autoscaler/reconciler.go`), guarded by a
`sync.Mutex` so only one cycle ever runs at a time:

1. Load all tests and all workers.
2. `desired := DesiredCapacity(tests)`, `pending := PendingCapacity(tests)`.
3. Count `actual` fleet size as every worker that is `IDLE`, `RESERVED`, or
   `RUNNING` (`OFFLINE` and `DRAINING` workers don't count as usable
   capacity). Idle workers are also collected separately as scale-down
   candidates.
4. `target := max(desired, pending)`.
5. If `actual < target` → **scale up** by `target - actual`.
   If `actual > target` → **scale down** by `actual - target`, but only
   from the idle pool.
   Otherwise, no-op.

```
target = max(desired, pending)

actual < target  →  provision (target - actual) workers
actual > target  →  drain     (actual - target) idle workers
actual == target →  no-op
```

### Scale-up

Delegates directly to `provisioner.Provision(ctx, count)`
(`ProcessProvisioner`), which starts that many `worker` processes with
generated hostnames (`vulcan-auto-worker-<timestamp>-<i>`) so they can be
identified as autoscaler-owned later.

### Scale-down

Only **idle** workers are candidates, and only if they're
**autoscaler-managed** — hostname prefixed `vulcan-auto-worker-`. This
matters in the Docker Compose deployment: the static `worker` service
(`worker-compose-1`) is started directly by Compose, not by the
provisioner, so `ProcessProvisioner.Terminate` has no PID for it. It's
excluded from the eligible pool so the reconciler doesn't waste a
termination attempt it can't fulfill (see [Known limitation](#known-limitation-the-compose-baseline-worker)
below for what happens if it's ever the *only* idle worker anyway).

Termination itself is two steps, deliberately kept separate:

1. **`TerminateIdleWorkers(ctx, workerIDs)`** — a conditional bulk
   `UPDATE ... WHERE status = 'IDLE'` that flips only the workers still
   actually idle to `DRAINING`, returning the ones that succeeded. This
   closes the race between the autoscaler reading a worker's status and
   the scheduler reserving it a moment later: once `DRAINING`, the
   scheduler's `ReserveWorkersForTest` (which only selects `status =
   'IDLE'`) can no longer pick it up.
2. **`provisioner.Terminate(ctx, hostnames)`** — actually kills the
   underlying processes. If this fails, the affected rows are
   **intentionally left `DRAINING`** rather than rolled back to `IDLE` —
   a worker whose process is supposed to be gone must never be handed back
   to the scheduler on the strength of a failed kill.

## Concurrency safety

- **One fleet-capacity policy.** `TestService.ensureWorkersAvailable`
  (triggered synchronously inside `StartTest` when a test needs more idle
  workers than exist) and the periodic autoscaler loop
  (`Reconciler.Run`, ticking every `AUTOSCALER_INTERVAL`, default `5s`)
  both call the *same* `Reconciler.Reconcile`, serialized by its internal
  mutex. There is no separate "test-start provisioning" logic that could
  race the autoscaler into double-provisioning — this was a real bug in
  an earlier phase, fixed by routing both paths through one reconciler.
- **Heartbeats can't undo scheduler/reconciler decisions.**
  `UpdateHeartbeat` only lets a heartbeat overwrite `IDLE`/`RUNNING` with
  `IDLE`/`RUNNING`; `RESERVED`, `DRAINING`, and `OFFLINE` are left
  untouched (though `last_heartbeat` is still refreshed for liveness
  tracking). Without this, a worker's stale local status could silently
  un-drain a worker the reconciler just marked `DRAINING`, or un-reserve
  one the scheduler just reserved, purely from a heartbeat race.
- **Scale-down never touches active work.** `RESERVED` and `RUNNING`
  workers are excluded from the idle candidate pool entirely — they are
  never drain candidates regardless of fleet target.

## The Phase 10 bug (pending capacity)

Before `PendingCapacity` existed, `DesiredCapacity` alone drove both
scale-up *and* scale-down. A test sitting in `CREATED` — already assigned
a `worker_count`, but with zero workers actually reserved — contributed
**nothing** to desired capacity. If it was the only test in the system, an
`IDLE` worker sitting around for it looked like pure excess capacity
(`desired=0, actual=1`), and the periodic reconciler would drain it before
`StartTest` ever got a chance to reserve it — permanently stranding the
test in `CREATED` with a worker it could never claim (compounded, in the
Compose deployment, by the baseline worker's `Terminate()` being a silent
no-op — see below).

The fix: compute `target := max(desired, pending)` instead of just
`desired`. A `CREATED` test's required workers now raise the scale-down
floor without triggering scale-up by themselves, closing the window
between "test created" and "test started" during which the fleet could
otherwise be drained to zero.

## Known limitation: the Compose baseline worker

The Docker Compose `worker` service (`worker-compose-1`) is a static,
always-on convenience worker started directly by Compose, not by
`ProcessProvisioner`. It is deliberately excluded from scale-down
eligibility (see [Scale-down](#scale-down) above) for exactly this reason —
but if fleet demand legitimately drops to zero while it's the *only*
worker in the system, and an autoscaler-managed worker happens to exist
alongside it, only the autoscaler-managed one is a valid drain target. In
the degenerate case where the baseline worker is somehow the only idle
worker being considered eligible, `Terminate()` has no process to kill for
it and treats it as already gone, leaving the row `DRAINING` and
permanently unavailable until the container restarts. In practice this is
avoided by not leaving the fleet at zero desired capacity for the
baseline's entire idle lifetime; it is a known, accepted limitation of a
single-host demo deployment rather than something this phase set out to
redesign.

## Configuration

| Variable | Effect |
|---|---|
| `WORKER_CAPACITY_RPS` | RPS one worker is assumed to sustain; the only knob in the `ceil(rps / capacity)` formula used by both the scheduler and the autoscaler |
| `AUTOSCALER_INTERVAL` | How often the periodic reconciliation loop runs (default `5s`) |
| `WORKER_BINARY_PATH` / `VULCAN_PROJECT_ROOT` | Where `ProcessProvisioner` finds and launches the worker binary for scale-up |
