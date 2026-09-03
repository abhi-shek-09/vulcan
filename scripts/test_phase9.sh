#!/bin/bash
#
# Phase 9 integration test: Worker Autoscaling & Capacity Management.
#
# Verifies, against a running control plane:
#   1. The control plane is reachable.
#   2. Creating a test calculates the correct desired worker capacity.
#   3. Starting the test causes the fleet to be provisioned up to that
#      capacity (via the Phase 8 immediate path, which now delegates to the
#      autoscaler instead of provisioning independently).
#   4. The system keeps operating normally (test reaches RUNNING).
#   5. After the test completes and its workers are released back to IDLE,
#      the periodic autoscaler reconciliation drains the now-excess idle
#      workers (IDLE -> DRAINING) instead of leaving them sitting around.
#
# Uses polling with a timeout instead of fixed sleeps wherever the timing
# depends on the autoscaler's reconciliation interval, since that interval
# is configurable (AUTOSCALER_INTERVAL) and shouldn't be hard-coded here.

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

BASE_URL="${BASE_URL:-http://localhost:8080}"
TARGET_URL="${TARGET_URL:-http://localhost:9090}"
RPS="${RPS:-350}"
CAPACITY="${WORKER_CAPACITY_RPS:-100}"
DURATION="${TEST_DURATION:-10}"
CONCURRENCY="${CONCURRENCY:-4}"

# Should comfortably exceed AUTOSCALER_INTERVAL so at least one background
# reconciliation cycle has a chance to run.
AUTOSCALER_POLL_TIMEOUT="${AUTOSCALER_POLL_TIMEOUT:-60}"
PROVISION_POLL_TIMEOUT="${PROVISION_POLL_TIMEOUT:-30}"

PASS=0
FAIL=0
TEST_ID=""

pass() { echo "PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "FAIL: $1"; FAIL=$((FAIL + 1)); }

# poll_until <timeout_seconds> <description> <command...>
# Repeats `command` (which should itself echo the value being polled and
# exit 0 once satisfied) every second until it succeeds or the timeout
# elapses.
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

count_workers_with_status() {
    local status="$1"
    curl -fsS "$BASE_URL/api/v1/workers" 2>/dev/null \
        | jq "[.[] | select(.status == \"$status\")] | length" 2>/dev/null
}

# --- 1. Control plane reachable ---

echo "Checking control plane..."
if curl -fsS "$BASE_URL/api/v1/tests" >/dev/null; then
    pass "Control Plane reachable"
else
    fail "Control Plane not reachable"
    echo
    echo "PHASE 9: FAILED"
    exit 1
fi

# Baseline idle worker count, so scale-down assertions later are relative
# to what already existed before this script ran (autoscaler / other tests
# may have left workers registered).
BASELINE_IDLE="$(count_workers_with_status IDLE)"
BASELINE_IDLE="${BASELINE_IDLE:-0}"
echo "Baseline idle workers: $BASELINE_IDLE"

# --- 2. Create a test and verify desired capacity ---

echo "Creating test requiring multiple workers (rps=$RPS, capacity=$CAPACITY)..."
CREATE_RESPONSE="$(
    curl -fsS \
        -X POST "$BASE_URL/api/v1/tests" \
        -H "Content-Type: application/json" \
        -d "{
            \"name\": \"phase9-autoscaler-integration\",
            \"target_url\": \"$TARGET_URL\",
            \"method\": \"GET\",
            \"duration_sec\": $DURATION,
            \"rps\": $RPS,
            \"concurrency\": $CONCURRENCY
        }"
)"

echo "$CREATE_RESPONSE" | jq .

TEST_ID="$(echo "$CREATE_RESPONSE" | jq -r '.id')"
REQUIRED="$(echo "$CREATE_RESPONSE" | jq -r '.worker_count')"

# ceil(350 / 100) = 4 with the defaults above.
EXPECTED_REQUIRED=$(( (RPS + CAPACITY - 1) / CAPACITY ))

if [ -n "$TEST_ID" ] && [ "$TEST_ID" != "null" ] && [ "$REQUIRED" = "$EXPECTED_REQUIRED" ]; then
    pass "Desired worker capacity calculated as $REQUIRED"
else
    fail "Expected worker_count $EXPECTED_REQUIRED, got '$REQUIRED'"
    echo
    echo "PHASE 9: FAILED"
    exit 1
fi

# --- 3. Starting the test provisions the fleet up to desired capacity ---

echo "Starting test (this should trigger fleet provisioning if needed)..."
START_RESPONSE="$(curl -fsS -X POST "$BASE_URL/api/v1/tests/$TEST_ID/start")"
echo "$START_RESPONSE" | jq .

ASSIGNED="$(echo "$START_RESPONSE" | jq '.workers | length' 2>/dev/null)"

if [ "$ASSIGNED" = "$REQUIRED" ]; then
    pass "$ASSIGNED workers provisioned and assigned to the test"
else
    fail "Expected $REQUIRED assigned workers, got '$ASSIGNED'"
fi

# The fleet (RESERVED + RUNNING + IDLE, excluding OFFLINE/DRAINING) must now
# be at least the required capacity.
check_fleet_capacity() {
    local total
    total="$(
        curl -fsS "$BASE_URL/api/v1/workers" 2>/dev/null \
            | jq '[.[] | select(.status == "IDLE" or .status == "RESERVED" or .status == "RUNNING")] | length' \
            2>/dev/null
    )"
    [ -n "$total" ] && [ "$total" -ge "$REQUIRED" ]
}

if poll_until "$PROVISION_POLL_TIMEOUT" "usable fleet capacity >= $REQUIRED" check_fleet_capacity; then
    pass "Usable worker fleet reached required capacity"
else
    fail "Worker fleet never reached required capacity"
fi

# --- 4. System keeps operating: test reaches RUNNING ---

STATUS="$(curl -fsS "$BASE_URL/api/v1/tests/$TEST_ID" | jq -r '.status')"
if [ "$STATUS" = "RUNNING" ]; then
    pass "Test entered RUNNING state"
else
    fail "Expected RUNNING, got '$STATUS'"
fi

# --- 5. Let the test finish, releasing its workers back to IDLE ---

echo "Waiting for test completion..."
FINAL_STATUS="$STATUS"
DEADLINE=$((SECONDS + DURATION + 20))

while [ "$SECONDS" -lt "$DEADLINE" ]; do
    FINAL_STATUS="$(curl -fsS "$BASE_URL/api/v1/tests/$TEST_ID" | jq -r '.status')"
    if [ "$FINAL_STATUS" = "COMPLETED" ] || [ "$FINAL_STATUS" = "FAILED" ] || [ "$FINAL_STATUS" = "STOPPED" ]; then
        break
    fi
    sleep 1
done

if [ "$FINAL_STATUS" = "COMPLETED" ]; then
    pass "Test completed normally"
else
    fail "Expected COMPLETED, got '$FINAL_STATUS'"
fi

# --- 6/7/8. Allow autoscaler reconciliation and verify excess idle workers
#            are drained (IDLE -> DRAINING) now that desired capacity is 0
#            for this test's demand ---

echo "Waiting for autoscaler to reconcile and drain now-excess idle workers..."

check_idle_drained() {
    local idle
    idle="$(count_workers_with_status IDLE)"
    idle="${idle:-999999}"
    # We expect idle count to settle back down to (at most) the baseline,
    # i.e. the workers this test caused to be provisioned should no longer
    # be sitting IDLE.
    [ "$idle" -le "$BASELINE_IDLE" ]
}

if poll_until "$AUTOSCALER_POLL_TIMEOUT" "idle worker count back to baseline ($BASELINE_IDLE)" check_idle_drained; then
    pass "Autoscaler drained excess idle workers"
else
    DRAINING="$(count_workers_with_status DRAINING)"
    if [ -n "$DRAINING" ] && [ "$DRAINING" -gt 0 ]; then
        pass "Autoscaler moved excess idle workers to DRAINING ($DRAINING draining)"
    else
        fail "Excess idle workers were not drained by the autoscaler within ${AUTOSCALER_POLL_TIMEOUT}s"
    fi
fi

echo
echo "Phase 9 Passed: $PASS"
echo "Phase 9 Failed: $FAIL"

if [ "$FAIL" -gt 0 ]; then
    echo "PHASE 9: FAILED"
    exit 1
fi

echo "PHASE 9: PASSED"
