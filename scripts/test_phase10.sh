#!/bin/bash

#
# Phase 10 integration test: Docker deployment end-to-end lifecycle.
#
# Assumes the Docker Compose stack (deployments/docker/docker-compose.yml)
# is already up:
#
#   cd deployments/docker
#   docker compose up -d
#
# Verifies:
#   1. Infrastructure containers are healthy.
#   2. Core API endpoints respond.
#   3. At least one worker is registered.
#   4. A CREATED-but-not-yet-started test does NOT lose its worker to the
#      autoscaler.
#   5. The pending test can subsequently start and complete.
#   6. A full E2E load test reaches COMPLETED.
#   7. Worker(s) return to IDLE after completion.
#   8. Telemetry reaches VictoriaMetrics.
#   9. Control-plane metrics endpoint returns data.
#  10. Postgres data survives a container restart.
#
# Requires:
#   - curl
#   - jq
#   - docker compose v2
#

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_DIR="$PROJECT_ROOT/deployments/docker"

BASE_URL="${BASE_URL:-http://localhost:8080}"
VM_URL="${VM_URL:-http://localhost:8428}"
TARGET_URL="${TARGET_URL:-http://host.docker.internal:9090}"

RPS="${RPS:-10}"
CAPACITY="${WORKER_CAPACITY_RPS:-100}"
DURATION="${TEST_DURATION:-5}"
CONCURRENCY="${CONCURRENCY:-2}"

E2E_TIMEOUT="${E2E_TIMEOUT:-60}"
METRICS_TIMEOUT="${METRICS_TIMEOUT:-30}"
PENDING_PROTECT_TIMEOUT="${PENDING_PROTECT_TIMEOUT:-15}"
PENDING_COMPLETE_TIMEOUT="${PENDING_COMPLETE_TIMEOUT:-15}"

PASS=0
FAIL=0

pass() {
    echo "PASS: $1"
    PASS=$((PASS + 1))
}

fail() {
    echo "FAIL: $1"
    FAIL=$((FAIL + 1))
}

compose() {
    (
        cd "$COMPOSE_DIR" &&
        docker compose "$@"
    )
}

# poll_until <timeout_seconds> <description> <command...>
poll_until() {
    local timeout="$1"
    local description="$2"

    shift 2

    local deadline=$((SECONDS + timeout))

    while [ "$SECONDS" -lt "$deadline" ]; do
        if "$@"; then
            return 0
        fi

        sleep 1
    done

    echo "TIMEOUT waiting for: $description"
    return 1
}

echo "=== Phase 10: Infrastructure health ==="

check_container_healthy() {
    local service="$1"
    local status

    status="$(
        compose ps --format json "$service" 2>/dev/null |
            jq -r 'if type=="array" then .[0] else . end | .Health // .State' 2>/dev/null
    )"

    [ "$status" = "healthy" ] || [ "$status" = "running" ]
}

for svc in postgres nats victoriametrics grafana control-plane; do
    if check_container_healthy "$svc"; then
        pass "$svc is healthy/running"
    else
        fail "$svc is not healthy"
    fi
done

echo

echo "=== Phase 10: API surface ==="

if curl -fsS "$BASE_URL/health" |
    jq -e '.status == "ok"' >/dev/null 2>&1; then

    pass "GET /health returns ok"

else
    fail "GET /health did not return ok"

    echo
    echo "PHASE 10: FAILED (control plane unreachable, aborting)"
    exit 1
fi

WORKERS_JSON="$(curl -fsS "$BASE_URL/api/v1/workers")"
WORKER_COUNT="$(echo "$WORKERS_JSON" | jq 'length' 2>/dev/null)"

if [ -n "$WORKER_COUNT" ] && [ "$WORKER_COUNT" -ge 1 ]; then
    pass "GET /api/v1/workers returns $WORKER_COUNT worker(s)"
else
    fail "GET /api/v1/workers returned no workers"

    echo
    echo "PHASE 10: FAILED (no workers registered, aborting)"
    exit 1
fi

echo

echo "=== Phase 10: Pending test does not lose its worker ==="

#
# Regression test for the Phase 10 lifecycle bug:
#
# A CREATED test contributes pending capacity so the autoscaler cannot
# incorrectly drain the currently available worker before the test is
# started.
#

count_workers_with_status() {
    local status="$1"

    curl -fsS "$BASE_URL/api/v1/workers" 2>/dev/null |
        jq "[.[] | select(.status == \"$status\")] | length" 2>/dev/null
}

PENDING_RESPONSE="$(
    curl -fsS \
        -X POST "$BASE_URL/api/v1/tests" \
        -H 'Content-Type: application/json' \
        -d "{
            \"name\": \"phase10-pending-protection\",
            \"target_url\": \"$TARGET_URL\",
            \"method\": \"GET\",
            \"rps\": $CAPACITY,
            \"duration_sec\": 1,
            \"concurrency\": 1
        }"
)"

echo "$PENDING_RESPONSE" | jq .

PENDING_TEST_ID="$(echo "$PENDING_RESPONSE" | jq -r '.id')"
PENDING_STATUS="$(echo "$PENDING_RESPONSE" | jq -r '.status')"

if [ -n "$PENDING_TEST_ID" ] &&
    [ "$PENDING_TEST_ID" != "null" ] &&
    [ "$PENDING_STATUS" = "CREATED" ]; then

    pass "Pending test created (id=$PENDING_TEST_ID, status=CREATED)"

else
    fail "Failed to create pending test: $PENDING_RESPONSE"
fi

echo
echo "Waiting ${PENDING_PROTECT_TIMEOUT}s (spanning multiple autoscaler cycles) without starting the test..."

sleep "$PENDING_PROTECT_TIMEOUT"

IDLE_AFTER_WAIT="$(count_workers_with_status IDLE)"
IDLE_AFTER_WAIT="${IDLE_AFTER_WAIT:-0}"

if [ "$IDLE_AFTER_WAIT" -ge 1 ]; then
    pass "At least one worker still IDLE after waiting with an unstarted pending test ($IDLE_AFTER_WAIT idle)"
else
    fail "No IDLE workers left -- the autoscaler drained the worker(s) a pending test needed"
fi

#
# Now start the pending test. This proves the worker that survived the
# protection window can still be reserved successfully.
#

START_PENDING="$(
    curl -fsS \
        -X POST "$BASE_URL/api/v1/tests/$PENDING_TEST_ID/start"
)"

echo "$START_PENDING" | jq .

if echo "$START_PENDING" |
    jq -e '.status == "RUNNING" and (.workers | length >= 1)' >/dev/null 2>&1; then

    pass "Pending test successfully started and claimed a worker"

else
    fail "Pending test could not be started: $START_PENDING"
fi

#
# Wait for the short pending regression test to finish before starting the
# main E2E test. This keeps the two test lifecycles isolated.
#

check_pending_completed() {
    local status

    status="$(
        curl -fsS \
            "$BASE_URL/api/v1/tests/$PENDING_TEST_ID" \
            2>/dev/null |
            jq -r '.status' 2>/dev/null
    )"

    [ "$status" = "COMPLETED" ]
}

if poll_until \
    "$PENDING_COMPLETE_TIMEOUT" \
    "pending test $PENDING_TEST_ID reaching COMPLETED" \
    check_pending_completed; then

    pass "Pending protection test reached COMPLETED"

else
    PENDING_FINAL_STATUS="$(
        curl -fsS \
            "$BASE_URL/api/v1/tests/$PENDING_TEST_ID" |
            jq -r '.status'
    )"

    fail "Pending protection test did not complete in time (last status: $PENDING_FINAL_STATUS)"
fi

echo

echo "=== Phase 10: Full E2E load test ==="

CREATE_RESPONSE="$(
    curl -fsS \
        -X POST "$BASE_URL/api/v1/tests" \
        -H 'Content-Type: application/json' \
        -d "{
            \"name\": \"phase10-e2e\",
            \"target_url\": \"$TARGET_URL\",
            \"method\": \"GET\",
            \"rps\": $RPS,
            \"duration_sec\": $DURATION,
            \"concurrency\": $CONCURRENCY
        }"
)"

echo "$CREATE_RESPONSE" | jq .

TEST_ID="$(echo "$CREATE_RESPONSE" | jq -r '.id')"

if [ -n "$TEST_ID" ] && [ "$TEST_ID" != "null" ]; then
    pass "E2E test created (id=$TEST_ID)"
else
    fail "E2E test creation failed: $CREATE_RESPONSE"

    echo
    echo "PHASE 10: FAILED"
    exit 1
fi

#
# Tests are explicitly created first and then started through the public
# /start endpoint. This is the intended Vulcan lifecycle.
#

START_RESPONSE="$(
    curl -sS \
        -X POST "$BASE_URL/api/v1/tests/$TEST_ID/start"
)"

echo "$START_RESPONSE" | jq .

if echo "$START_RESPONSE" |
    jq -e '.status == "RUNNING" and (.workers | length >= 1)' >/dev/null 2>&1; then

    pass "E2E test transitioned into RUNNING with assigned worker(s)"

else
    fail "E2E test did not start correctly: $START_RESPONSE"
fi

check_test_completed() {
    local status

    status="$(
        curl -fsS \
            "$BASE_URL/api/v1/tests/$TEST_ID" \
            2>/dev/null |
            jq -r '.status' 2>/dev/null
    )"

    [ "$status" = "COMPLETED" ]
}

if poll_until \
    "$E2E_TIMEOUT" \
    "test $TEST_ID reaching COMPLETED" \
    check_test_completed; then

    pass "E2E test reached COMPLETED"

else
    FINAL_STATUS="$(
        curl -fsS \
            "$BASE_URL/api/v1/tests/$TEST_ID" |
            jq -r '.status'
    )"

    fail "E2E test did not complete in time (last status: $FINAL_STATUS)"
fi

echo

echo "=== Phase 10: Worker returns to IDLE ==="

check_worker_idle_again() {
    local idle

    idle="$(count_workers_with_status IDLE)"
    idle="${idle:-0}"

    [ "$idle" -ge 1 ]
}

if poll_until \
    15 \
    "a worker returning to IDLE after test completion" \
    check_worker_idle_again; then

    pass "Worker(s) returned to IDLE after test completion"

else
    fail "No worker returned to IDLE after test completion"
fi

echo

echo "=== Phase 10: Telemetry reaches VictoriaMetrics ==="

check_metrics_present() {
    local result

    result="$(
        curl -fsS \
            -G "$VM_URL/api/v1/query" \
            --data-urlencode "query=vulcan_requests_total{test_id=\"$TEST_ID\"}" \
            2>/dev/null |
            jq -r '.data.result | length' 2>/dev/null
    )"

    [ -n "$result" ] && [ "$result" -ge 1 ]
}

if poll_until \
    "$METRICS_TIMEOUT" \
    "vulcan_requests_total for test $TEST_ID in VictoriaMetrics" \
    check_metrics_present; then

    pass "VictoriaMetrics contains telemetry for the completed test"

else
    fail "No telemetry found in VictoriaMetrics for test $TEST_ID"
fi

if curl -fsS \
    "$BASE_URL/api/v1/tests/$TEST_ID/metrics" |
    jq -e '.' >/dev/null 2>&1; then

    pass "Control plane metrics endpoint returns data for the test"

else
    fail "Control plane metrics endpoint failed for the test"
fi

echo

echo "=== Phase 10: Persistence across restart ==="

echo "Restarting postgres and control-plane containers..."

compose restart postgres control-plane >/dev/null 2>&1

check_control_plane_back_up() {
    curl -fsS "$BASE_URL/health" >/dev/null 2>&1
}

if poll_until \
    60 \
    "control plane back up after restart" \
    check_control_plane_back_up; then

    pass "Control plane came back up after restart"

else
    fail "Control plane did not come back up after restart"

    echo
    echo "PHASE 10: FAILED"
    exit 1
fi

PERSISTED_TEST="$(
    curl -fsS \
        "$BASE_URL/api/v1/tests/$TEST_ID" \
        2>/dev/null |
        jq -r '.id' 2>/dev/null
)"

if [ "$PERSISTED_TEST" = "$TEST_ID" ]; then
    pass "Test record for $TEST_ID survived the Postgres/control-plane restart"
else
    fail "Test record for $TEST_ID was lost after restart"
fi

echo
echo "Phase 10 Passed: $PASS"
echo "Phase 10 Failed: $FAIL"

if [ "$FAIL" -gt 0 ]; then
    echo "PHASE 10: FAILED"
    exit 1
fi

echo "PHASE 10: PASSED"