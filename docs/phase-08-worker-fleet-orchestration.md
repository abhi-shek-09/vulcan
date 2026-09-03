# Phase 8 — Worker Fleet Orchestration & Lifecycle Management

## Overview

Phase 8 completes the worker-fleet lifecycle for Vulcan.

The goal was to make the complete test lifecycle reliable when a load test is distributed across multiple workers:

1. Create a test.
2. Calculate the required worker count from requested RPS.
3. Provision missing workers.
4. Register workers with the control plane.
5. Allocate workers and distribute RPS.
6. Execute the assigned load.
7. Report assignment completion.
8. Complete the parent test only after all assignments complete.
9. Handle assignment and test failures correctly.
10. Verify the complete lifecycle with a multi-worker integration test.

Phase 8 also exposed and fixed an important PostgreSQL concurrency race in the assignment-completion path.

---

## 1. Worker Count Calculation

The test API does not require the caller to explicitly provide the worker count.

The control plane calculates the required worker count from:

- requested test RPS
- configured per-worker RPS capacity

For example:

```text
Requested RPS = 350
Worker capacity = 100 RPS

required workers = ceil(350 / 100)
                 = 4
```

The Phase 8 integration test verifies that a test created without an explicit worker count receives:

```text
worker_count = 4
```

---

## 2. Worker Provisioning

When a test starts, the control plane checks the currently registered idle workers.

If there are not enough workers, the provisioner creates the missing workers.

The control plane then waits for enough workers to register before allocation continues.

The resulting flow is:

```text
Start Test
    |
    v
Calculate required workers
    |
    v
Count idle workers
    |
    +---- enough ----> Allocate
    |
    +---- insufficient
              |
              v
        Provision missing
              |
              v
        Wait for registration
              |
              v
           Allocate
```

---

## 3. Distributed RPS Allocation

Once the required workers are available, the scheduler distributes the requested RPS across them.

The Phase 8 integration test used:

```text
Total RPS = 350
Workers   = 4
```

and produced:

```text
88
88
87
87
```

The allocation preserves the requested total:

```text
88 + 88 + 87 + 87 = 350
```

---

## 4. Assignment Lifecycle

Each test/worker relationship is represented in `test_workers`.

The normal assignment lifecycle is:

```text
RESERVED
   |
   v
RUNNING
   |
   v
COMPLETED
```

A failure can occur from either `RESERVED` or `RUNNING`:

```text
RESERVED ----                             > FAILED
              /
RUNNING ------/
```

### RESERVED

The scheduler has allocated the worker to the test, but execution has not started yet.

### RUNNING

The worker has accepted the assignment and started execution.

### COMPLETED

The worker successfully finished its assigned execution.

### FAILED

The assignment could not successfully execute, or the worker disappeared before it could transition into `RUNNING`.

---

## 5. Parent Test Lifecycle

A normal test lifecycle is:

```text
CREATED
   |
   v
STARTING
   |
   v
RUNNING
   |
   v
COMPLETED
```

Failure:

```text
RUNNING
   |
   v
FAILED
```

Stopping:

```text
RUNNING
   |
   v
STOPPING
   |
   v
STOPPED
```

The important invariant is:

> A test may become `COMPLETED` only after every worker assignment belonging to that test has become `COMPLETED`.

---

# 6. Assignment Completion

The main completion logic is implemented in:

```text
internal/repository/assignment.go
```

specifically:

```text
MarkAssignmentCompleted()
```

The function performs the following operations inside one PostgreSQL transaction:

1. Mark the current assignment as `COMPLETED`.
2. Lock the assignment rows belonging to the same test.
3. Check whether any assignment remains incomplete.
4. If none remain, mark the parent test as `COMPLETED`.
5. Commit.

Conceptually:

```text
BEGIN
   |
   v
Mark assignment COMPLETED
   |
   v
Lock assignments for this test
   |
   v
Any incomplete assignments?
   |
   +---- YES ----> Commit
   |
   +---- NO -----> Mark test COMPLETED
                       |
                       v
                    Commit
```

---

# 7. The Concurrency Bug

Phase 8 exposed a race condition when multiple workers completed at approximately the same time.

The original implementation performed:

```sql
UPDATE test_workers
SET status = 'COMPLETED'
WHERE test_id = $1
  AND worker_id = $2
  AND status = 'RUNNING';
```

followed by:

```sql
SELECT EXISTS (
    SELECT 1
    FROM test_workers
    WHERE test_id = $1
      AND status != 'COMPLETED'
);
```

Both operations were inside a transaction, but PostgreSQL's default `READ COMMITTED` isolation was not enough to guarantee the required completion barrier.

---

# 8. How the Race Happened

Consider a test with two workers:

```text
Worker A
Worker B
```

Both finish at nearly the same time.

Two transactions can overlap:

```text
Transaction A                    Transaction B
---------------------------------------------------------
BEGIN                            BEGIN

UPDATE A -> COMPLETED            UPDATE B -> COMPLETED

SELECT incomplete assignments    SELECT incomplete assignments

A sees B as RUNNING              B sees A as RUNNING

A decides test is incomplete    B decides test is incomplete

COMMIT                           COMMIT
```

Each transaction can see its own uncommitted update, but not the other transaction's uncommitted update.

Therefore Transaction A can observe:

```text
A = COMPLETED
B = RUNNING
```

while Transaction B can observe:

```text
A = RUNNING
B = COMPLETED
```

Both conclude:

```text
There is still an incomplete assignment.
```

Neither executes the parent-test completion update.

After both transactions commit, the database can contain:

```text
test_workers

A -> COMPLETED
B -> COMPLETED
```

while:

```text
tests

test -> RUNNING
```

This leaves the test permanently stuck in `RUNNING` even though every worker assignment is `COMPLETED`.

---

# 9. Why the Bug Was Easy to Miss

The race does not normally appear with a single worker.

For one worker:

```text
Worker A -> COMPLETED
```

there is no competing completion transaction.

The final completion check correctly sees no incomplete assignments and changes the parent test to:

```text
COMPLETED
```

The problem requires multiple workers finishing concurrently.

Therefore a single-worker integration test can pass while the distributed completion barrier remains incorrect.

---

# 10. The Fix

The completion check was changed to lock the assignment rows for the test before evaluating their status:

```sql
SELECT EXISTS (
    SELECT 1
    FROM (
        SELECT status
        FROM test_workers
        WHERE test_id = $1
        FOR UPDATE
    ) assignments
    WHERE status != 'COMPLETED'
);
```

The important part is:

```sql
FOR UPDATE
```

This prevents concurrent completion transactions from independently performing the final completion check against stale concurrent state.

The completion checks for assignments belonging to the same test are effectively serialized.

---

# 11. Why `FOR UPDATE` Fixes It

Suppose Transaction A completes Worker A first and then locks the assignment rows for the test.

Transaction B attempts the same completion check.

Because Transaction A holds the relevant row locks, Transaction B waits.

After Transaction A commits, Transaction B can continue and observe the committed state.

For the final worker, the database can therefore correctly reach:

```text
Worker A = COMPLETED
Worker B = COMPLETED
Worker C = COMPLETED
Worker D = COMPLETED
```

The completion check then finds:

```text
no incomplete assignments
```

and transitions the parent test to:

```text
COMPLETED
```

---

# 12. Why the State Machine Was Not Changed

The state machine itself was not the source of the bug.

The existing flow was correct:

```text
Worker
  |
  v
StartAssignment
  |
  v
RUNNING
  |
  v
Execute
  |
  v
CompleteAssignment
  |
  v
MarkAssignmentCompleted
```

The problem was the concurrent database check inside `MarkAssignmentCompleted`.

The fix was therefore deliberately kept local to:

```text
internal/repository/assignment.go
```

rather than changing:

- worker execution
- assignment service
- scheduler
- test service
- assignment API
- worker state transitions

This keeps the concurrency guarantee at the database layer where the shared state actually exists.

---

# 13. Failure Handling

`MarkAssignmentFailed` handles assignments that fail while either:

```text
RESERVED
```

or:

```text
RUNNING
```

This is necessary because a worker can disappear after receiving an assignment but before successfully transitioning it to `RUNNING`.

When an assignment fails:

```text
assignment -> FAILED
test       -> FAILED
```

The parent test is transitioned atomically with the assignment failure so it does not remain indefinitely in `RUNNING`.

---

# 14. Stop Handling

Stopping is handled separately from execution failure.

The control plane transitions the test toward:

```text
STOPPING
```

and releases its workers.

Workers can detect the stop while polling the control plane and terminate their execution.

The worker uses:

```go
ErrAssignmentStopped
```

to distinguish an externally stopped assignment from an actual execution failure.

A normal user-initiated stop therefore does not get incorrectly reported as:

```text
FAILED
```

---

# 15. Worker Execution Lifecycle

The worker lifecycle is:

```text
IDLE
  |
  v
Poll for assignment
  |
  v
Assignment received
  |
  v
Mark assignment RUNNING
  |
  v
Execute HTTP load
  |
  +---- error ------> FailAssignment
  |
  +---- stopped ----> Return to IDLE
  |
  +---- success ----> CompleteAssignment
                          |
                          v
                       IDLE
```

The worker becomes available again after successful completion or handled termination.

---

# 16. NATS Configuration Issue During Testing

During Phase 8 testing, the control-plane container initially failed with:

```text
NATS_URL environment variable is required
```

The repository contains the NATS configuration in:

```text
deployments/docker/docker-compose.yml
```

where containerized services use:

```yaml
NATS_URL: nats://nats:4222
```

The local `.env` contains:

```text
NATS_URL=nats://localhost:4222
```

These values are intentionally different.

From the host machine:

```text
nats://localhost:4222
```

From one Docker Compose service to another:

```text
nats://nats:4222
```

Inside Docker, `localhost` refers to the current container, not the NATS container. The Compose service name `nats` is therefore used for container-to-container communication.

---

# 17. Phase 8 Integration Test

The integration test is:

```text
scripts/test_phase8.sh
```

It verifies:

### Control plane

```text
PASS: Control Plane reachable
```

### Test creation

A test is created without specifying `worker_count`.

The response confirms:

```text
worker_count = 4
```

### Test start

The test transitions to:

```text
RUNNING
```

and four workers are assigned.

### RPS allocation

The workers receive:

```text
88
88
87
87
```

The script verifies:

```text
88 + 88 + 87 + 87 = 350
```

### Completion

The test eventually reaches:

```text
COMPLETED
```

The final integration result is:

```text
PASS: Test completed normally

Phase 8 Passed: 6
Phase 8 Failed: 0
PHASE 8: PASSED
```

---

# 18. Final Verification

The final Phase 8 integration test passed completely:

```text
PASS: Required workers calculated as 4
PASS: 4 workers assigned
PASS: Test entered RUNNING state
PASS: Worker RPS allocations sum to 350
PASS: Test completed normally

Phase 8 Passed: 6
Phase 8 Failed: 0
PHASE 8: PASSED
```

The previously observed failure:

```text
Expected COMPLETED, got FAILED
```

was separately traced to the worker/control-plane runtime configuration issue involving `NATS_URL`. Once the environment was corrected, the Phase 8 lifecycle completed successfully.

---

# 19. Important Invariants

Phase 8 establishes the following invariants.

### Worker count

```text
required workers = ceil(total RPS / worker capacity)
```

### RPS conservation

```text
sum(worker RPS allocations) = requested RPS
```

### Assignment lifecycle

```text
RESERVED -> RUNNING -> COMPLETED
```

### Failure lifecycle

```text
RESERVED/RUNNING -> FAILED
```

### Parent completion

```text
test = COMPLETED
    only when
all assignments = COMPLETED
```

### Parent failure

```text
assignment = FAILED
    =>
test = FAILED
```

### Concurrent completion safety

Multiple workers completing simultaneously must not leave the parent test stuck in:

```text
RUNNING
```

when all assignments are actually:

```text
COMPLETED
```

---

# 20. Most Relevant Phase 8 Files

The main Phase 8 implementation areas are:

```text
internal/service/test_service.go
internal/service/assignment_transition.go
internal/repository/assignment.go
internal/repository/worker_repository.go
internal/worker/worker.go
internal/worker/executor.go
internal/scheduler/
internal/provisioner/
internal/controlplane/client.go
deployments/docker/docker-compose.yml
scripts/test_phase8.sh
```

The concurrency fix is specifically in:

```text
internal/repository/assignment.go
```

inside:

```text
MarkAssignmentCompleted()
```

---

# 21. Lessons Learned

The major lesson from Phase 8 is:

> A transaction is not automatically safe for concurrent workflows simply because all operations are inside the same transaction.

The original logic was effectively:

```text
BEGIN
UPDATE assignment
SELECT remaining assignments
UPDATE parent test
COMMIT
```

The missing piece was serialization between concurrent completion transactions.

The application invariant:

```text
"The last worker to finish completes the parent test."
```

must be backed by a database concurrency mechanism.

In this case, locking the relevant assignment rows with:

```sql
FOR UPDATE
```

was sufficient to serialize the completion checks for a given test.

The bug was therefore not caused by:

- incorrect worker IDs
- incorrect test IDs
- incorrect HTTP routing
- incorrect assignment transitions
- incorrect RPS distribution
- incorrect execution duration
- incorrect worker registration

It was a database concurrency race in the final aggregation of worker completion state.

---

# 22. Phase 8 Completion Status

**Phase 8 is complete.**

The worker fleet can now reliably:

```text
Provision
   |
Register
   |
Allocate
   |
Execute
   |
Report completion
   |
Safely aggregate worker completion
   |
Complete parent test
```

The multi-worker integration test passes successfully, including the previously failing concurrent completion scenario.
