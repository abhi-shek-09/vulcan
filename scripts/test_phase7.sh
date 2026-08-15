#!/bin/bash

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$PROJECT_ROOT"

BASE_URL="${BASE_URL:-http://localhost:8080}"
TARGET_URL="${TARGET_URL:-http://localhost:9090}"
NATS_URL="${NATS_URL:-nats://localhost:4222}"

WORKER_COUNT="${WORKER_COUNT:-3}"
TEST_DURATION="${TEST_DURATION:-15}"
RPS="${RPS:-30}"
CONCURRENCY="${CONCURRENCY:-2}"

WORKER_PIDS=()
WORKER_LOG_DIR="${WORKER_LOG_DIR:-/tmp/vulcan-phase7-workers}"
WORKER_BIN=""

WORKER_YAML="$PROJECT_ROOT/configs/worker.yaml"
WORKER_YAML_BACKUP=""

PASS=0
FAIL=0

TEST_ID=""

log() {
    echo
    echo "----------------------------------------------------"
    echo "$1"
    echo "----------------------------------------------------"
}

pass() {
    echo "PASS: $1"
    PASS=$((PASS + 1))
}

fail() {
    echo "FAIL: $1"
    FAIL=$((FAIL + 1))
}

cleanup() {
    log "Cleaning up Phase 7 workers"

    for pid in "${WORKER_PIDS[@]}"; do
        if kill -0 "$pid" 2>/dev/null; then
            echo "Stopping worker process $pid"
            kill "$pid" 2>/dev/null || true
        fi
    done

    sleep 1

    for pid in "${WORKER_PIDS[@]}"; do
        if kill -0 "$pid" 2>/dev/null; then
            kill -9 "$pid" 2>/dev/null || true
        fi
    done

    # Restore configs/worker.yaml if we patched it to point at BASE_URL.
    if [ -n "$WORKER_YAML_BACKUP" ] && [ -f "$WORKER_YAML_BACKUP" ]; then
        cp "$WORKER_YAML_BACKUP" "$WORKER_YAML"
        rm -f "$WORKER_YAML_BACKUP"
    fi

    echo
    echo "Worker logs:"
    echo "  $WORKER_LOG_DIR"

    echo
    echo "----------------------------------------------------"
    echo "Phase 7 Test Summary"
    echo "----------------------------------------------------"
    echo "Passed: $PASS"
    echo "Failed: $FAIL"

    if [ "$FAIL" -gt 0 ]; then
        echo
        echo "PHASE 7: FAILED"
        exit 1
    fi

    echo
    echo "PHASE 7: PASSED"
}

trap cleanup EXIT INT TERM


# ----------------------------------------------------
# Requirements
# ----------------------------------------------------

if ! command -v curl >/dev/null 2>&1; then
    echo "ERROR: curl is required"
    exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
    echo "ERROR: jq is required"
    exit 1
fi

if ! command -v go >/dev/null 2>&1; then
    echo "ERROR: Go is required"
    exit 1
fi


# ----------------------------------------------------
# Control Plane
# ----------------------------------------------------

log "Checking Control Plane"

if curl -fsS "$BASE_URL/api/v1/tests" >/dev/null; then
    pass "Control Plane reachable"
else
    fail "Control Plane is not reachable"
    exit 1
fi


# ----------------------------------------------------
# Point workers at BASE_URL
# ----------------------------------------------------

# configs/worker.yaml is the ONLY source of control_plane.url -- the
# worker process has no env-var override for it (only WORKER_HOSTNAME
# is read from the environment). Without this, overriding BASE_URL
# would silently do nothing and workers would keep talking to whatever
# URL happens to be baked into the yaml file.
log "Pointing configs/worker.yaml at $BASE_URL"

if [ ! -f "$WORKER_YAML" ]; then
    fail "configs/worker.yaml not found at $WORKER_YAML"
    exit 1
fi

WORKER_YAML_BACKUP="$(mktemp)"
cp "$WORKER_YAML" "$WORKER_YAML_BACKUP"

sed -i.bak "s#^\( *url: *\).*#\1$BASE_URL#" "$WORKER_YAML" && rm -f "$WORKER_YAML.bak"

pass "configs/worker.yaml control_plane.url set to $BASE_URL"


# ----------------------------------------------------
# Worker startup
# ----------------------------------------------------

log "Starting Phase 7 workers"

mkdir -p "$WORKER_LOG_DIR"

# Build the worker once and run the resulting binary directly for each
# worker instance. Using `go run` here would mean $! is the PID of the
# `go` wrapper, not the actual worker process -- SIGKILL on that PID
# does not reliably kill the compiled child, leaving orphaned workers
# behind (and making it useless if you ever need to kill a worker to
# test the OFFLINE/FAILED reconciliation path).
WORKER_BIN="$WORKER_LOG_DIR/worker-bin"

echo "Building worker binary..."
if ! go build -o "$WORKER_BIN" ./cmd/worker; then
    fail "Failed to build worker binary"
    exit 1
fi

for i in $(seq 1 "$WORKER_COUNT"); do

    HOSTNAME="phase7-worker-$i"
    LOG_FILE="$WORKER_LOG_DIR/$HOSTNAME.log"

    echo "Starting $HOSTNAME"

    (
        cd "$PROJECT_ROOT" && \
        WORKER_HOSTNAME="$HOSTNAME" \
        NATS_URL="$NATS_URL" \
        "$WORKER_BIN"
    ) >"$LOG_FILE" 2>&1 &

    PID=$!

    WORKER_PIDS+=("$PID")

    echo "  PID: $PID"
    echo "  Log: $LOG_FILE"

done


# ----------------------------------------------------
# Wait for worker registration
# ----------------------------------------------------

log "Waiting for workers to register"

MAX_WAIT=30
ELAPSED=0

REGISTERED_COUNT=0

while [ "$ELAPSED" -lt "$MAX_WAIT" ]; do

    WORKERS_JSON="$(
        curl -fsS "$BASE_URL/api/v1/workers" 2>/dev/null || echo "[]"
    )"

    REGISTERED_COUNT="$(
        echo "$WORKERS_JSON" |
        jq --arg prefix "phase7-worker-" \
        '[.[] | select(.hostname | startswith($prefix))] | length' 2>/dev/null
    )"
    REGISTERED_COUNT="${REGISTERED_COUNT:-0}"

    echo "Registered Phase 7 workers: $REGISTERED_COUNT/$WORKER_COUNT"

    if [ "$REGISTERED_COUNT" -ge "$WORKER_COUNT" ]; then
        break
    fi

    sleep 1
    ELAPSED=$((ELAPSED + 1))
done


if [ "$REGISTERED_COUNT" -ge "$WORKER_COUNT" ]; then
    pass "$WORKER_COUNT Phase 7 workers registered"
else
    fail "Expected $WORKER_COUNT workers, found $REGISTERED_COUNT"

    echo
    echo "Worker logs:"
    for i in $(seq 1 "$WORKER_COUNT"); do
        HOSTNAME="phase7-worker-$i"
        LOG_FILE="$WORKER_LOG_DIR/$HOSTNAME.log"

        echo
        echo "===== $HOSTNAME ====="
        cat "$LOG_FILE" 2>/dev/null || true
    done

    exit 1
fi


# ----------------------------------------------------
# Wait for workers to become IDLE
# ----------------------------------------------------

log "Waiting for workers to become IDLE"

MAX_WAIT=30
ELAPSED=0

IDLE_COUNT=0

while [ "$ELAPSED" -lt "$MAX_WAIT" ]; do

    WORKERS_JSON="$(
        curl -fsS "$BASE_URL/api/v1/workers" 2>/dev/null || echo "[]"
    )"

    IDLE_COUNT="$(
        echo "$WORKERS_JSON" |
        jq --arg prefix "phase7-worker-" \
        '[.[] |
            select(.hostname | startswith($prefix)) |
            select(.status == "IDLE")
        ] | length'
    )"

    echo "IDLE Phase 7 workers: $IDLE_COUNT/$WORKER_COUNT"

    if [ "$IDLE_COUNT" -ge "$WORKER_COUNT" ]; then
        break
    fi

    sleep 1
    ELAPSED=$((ELAPSED + 1))
done


if [ "$IDLE_COUNT" -ge "$WORKER_COUNT" ]; then
    pass "$WORKER_COUNT Phase 7 workers are IDLE"
else
    fail "Not enough Phase 7 workers became IDLE"

    echo
    echo "$WORKERS_JSON" | jq .

    exit 1
fi


# ----------------------------------------------------
# Create test
# ----------------------------------------------------

log "Creating Phase 7 multi-worker test"

CREATE_RESPONSE="$(
    curl -fsS \
        -X POST \
        "$BASE_URL/api/v1/tests" \
        -H "Content-Type: application/json" \
        -d "{
            \"name\": \"phase7-integration\",
            \"target_url\": \"$TARGET_URL\",
            \"method\": \"GET\",
            \"duration_sec\": $TEST_DURATION,
            \"rps\": $RPS,
            \"concurrency\": $CONCURRENCY,
            \"worker_count\": $WORKER_COUNT
        }"
)"

echo "$CREATE_RESPONSE" | jq .

TEST_ID="$(
    echo "$CREATE_RESPONSE" |
    jq -r '.id'
)"

if [ -n "$TEST_ID" ] && [ "$TEST_ID" != "null" ]; then
    pass "Test created: $TEST_ID"
else
    fail "Test creation failed"
    exit 1
fi


# ----------------------------------------------------
# Verify configuration
# ----------------------------------------------------

log "Verifying test configuration"

CREATED_WORKERS="$(
    echo "$CREATE_RESPONSE" |
    jq -r '.worker_count'
)"

CREATED_RPS="$(
    echo "$CREATE_RESPONSE" |
    jq -r '.rps'
)"

CREATED_CONCURRENCY="$(
    echo "$CREATE_RESPONSE" |
    jq -r '.concurrency'
)"

if [ "$CREATED_WORKERS" = "$WORKER_COUNT" ]; then
    pass "worker_count=$WORKER_COUNT"
else
    fail "Expected worker_count=$WORKER_COUNT, got $CREATED_WORKERS"
fi

if [ "$CREATED_RPS" = "$RPS" ]; then
    pass "rps=$RPS"
else
    fail "Expected rps=$RPS, got $CREATED_RPS"
fi

if [ "$CREATED_CONCURRENCY" = "$CONCURRENCY" ]; then
    pass "concurrency=$CONCURRENCY"
else
    fail "Expected concurrency=$CONCURRENCY, got $CREATED_CONCURRENCY"
fi


# ----------------------------------------------------
# Duplicate start protection
# ----------------------------------------------------

log "Testing duplicate start protection"

START_ONE_BODY="$(mktemp)"
START_TWO_BODY="$(mktemp)"

START_ONE_STATUS="$(mktemp)"
START_TWO_STATUS="$(mktemp)"

curl -s \
    -o "$START_ONE_BODY" \
    -w "%{http_code}" \
    -X POST \
    "$BASE_URL/api/v1/tests/$TEST_ID/start" \
    >"$START_ONE_STATUS" &

PID_ONE=$!

curl -s \
    -o "$START_TWO_BODY" \
    -w "%{http_code}" \
    -X POST \
    "$BASE_URL/api/v1/tests/$TEST_ID/start" \
    >"$START_TWO_STATUS" &

PID_TWO=$!

wait "$PID_ONE"
wait "$PID_TWO"

STATUS_ONE="$(cat "$START_ONE_STATUS")"
STATUS_TWO="$(cat "$START_TWO_STATUS")"

echo
echo "Start request #1:"
cat "$START_ONE_BODY"
echo
echo "HTTP: $STATUS_ONE"

echo
echo "Start request #2:"
cat "$START_TWO_BODY"
echo
echo "HTTP: $STATUS_TWO"

if {
    [ "$STATUS_ONE" = "200" ] &&
    [ "$STATUS_TWO" = "409" ]
} || {
    [ "$STATUS_ONE" = "409" ] &&
    [ "$STATUS_TWO" = "200" ]
}; then

    pass "Duplicate start protection returned exactly one 200 and one 409"

else

    fail "Expected one 200 and one 409, got $STATUS_ONE and $STATUS_TWO"

fi

# The winning /start call is the ONLY response that carries a `workers`
# array (StartTestResponse). GET /tests/{id} returns models.Test, which
# has no `workers` field at all -- checking it there always evaluates
# against null. Capture the real payload here before we discard it.
if [ "$STATUS_ONE" = "200" ]; then
    START_WORKERS_JSON="$(cat "$START_ONE_BODY")"
elif [ "$STATUS_TWO" = "200" ]; then
    START_WORKERS_JSON="$(cat "$START_TWO_BODY")"
else
    START_WORKERS_JSON="{}"
fi

rm -f \
    "$START_ONE_BODY" \
    "$START_TWO_BODY" \
    "$START_ONE_STATUS" \
    "$START_TWO_STATUS"


# ----------------------------------------------------
# Verify RUNNING
# ----------------------------------------------------

log "Checking test entered RUNNING state"

sleep 2

RUNNING_RESPONSE="$(
    curl -fsS "$BASE_URL/api/v1/tests/$TEST_ID"
)"

echo "$RUNNING_RESPONSE" | jq .

CURRENT_STATUS="$(
    echo "$RUNNING_RESPONSE" |
    jq -r '.status'
)"

if [ "$CURRENT_STATUS" = "RUNNING" ]; then
    pass "Test entered RUNNING state"
else
    fail "Expected RUNNING, got $CURRENT_STATUS"
fi


# ----------------------------------------------------
# Verify worker assignment count
# ----------------------------------------------------

log "Checking worker assignments"

echo "$START_WORKERS_JSON" | jq .

ASSIGNED_COUNT="$(
    echo "$START_WORKERS_JSON" |
    jq '.workers | length'
)"

if [ "$ASSIGNED_COUNT" = "$WORKER_COUNT" ]; then
    pass "$WORKER_COUNT workers assigned"
else
    fail "Expected $WORKER_COUNT workers, got $ASSIGNED_COUNT"
fi


# ----------------------------------------------------
# Verify unique worker assignments
# ----------------------------------------------------

log "Checking worker assignment uniqueness"

UNIQUE_WORKERS="$(
    echo "$START_WORKERS_JSON" |
    jq '[.workers[].id] | unique | length'
)"

if [ "$UNIQUE_WORKERS" = "$WORKER_COUNT" ]; then
    pass "All assigned workers are distinct"
else
    fail "Duplicate worker assignment detected"
fi


# ----------------------------------------------------
# Verify worker hostnames
# ----------------------------------------------------

PHASE7_WORKERS="$(
    echo "$START_WORKERS_JSON" |
    jq -r '.workers[].hostname'
)"

echo
echo "Assigned workers:"
echo "$PHASE7_WORKERS"


# ----------------------------------------------------
# Wait for completion
# ----------------------------------------------------

log "Waiting for test completion"

MAX_WAIT=$((TEST_DURATION + 30))
ELAPSED=0

FINAL_STATUS=""

while [ "$ELAPSED" -lt "$MAX_WAIT" ]; do

    STATUS_RESPONSE="$(
        curl -fsS "$BASE_URL/api/v1/tests/$TEST_ID"
    )"

    FINAL_STATUS="$(
        echo "$STATUS_RESPONSE" |
        jq -r '.status'
    )"

    echo "Elapsed: ${ELAPSED}s | status: $FINAL_STATUS"

    if [ "$FINAL_STATUS" = "COMPLETED" ]; then
        break
    fi

    if [ "$FINAL_STATUS" = "FAILED" ]; then
        echo
        echo "Test unexpectedly FAILED:"
        echo "$STATUS_RESPONSE" | jq .
        break
    fi

    sleep 2
    ELAPSED=$((ELAPSED + 2))
done


if [ "$FINAL_STATUS" = "COMPLETED" ]; then
    pass "Test completed successfully"
else
    fail "Test did not complete successfully (final status: $FINAL_STATUS)"
fi


# ----------------------------------------------------
# Final test state
# ----------------------------------------------------

log "Checking final test state"

FINAL_RESPONSE="$(
    curl -fsS "$BASE_URL/api/v1/tests/$TEST_ID"
)"

echo "$FINAL_RESPONSE" | jq .

FINAL_TEST_STATUS="$(
    echo "$FINAL_RESPONSE" |
    jq -r '.status'
)"

if [ "$FINAL_TEST_STATUS" = "COMPLETED" ]; then
    pass "Final test status is COMPLETED"
else
    fail "Final test status is $FINAL_TEST_STATUS"
fi


# ----------------------------------------------------
# Final worker health
# ----------------------------------------------------

log "Checking worker health after test"

sleep 2

FINAL_WORKERS_JSON="$(
    curl -fsS "$BASE_URL/api/v1/workers"
)"

PHASE7_IDLE_COUNT="$(
    echo "$FINAL_WORKERS_JSON" |
    jq --arg prefix "phase7-worker-" \
    '[.[] |
        select(.hostname | startswith($prefix)) |
        select(.status == "IDLE")
    ] | length'
)"

echo "$FINAL_WORKERS_JSON" |
    jq --arg prefix "phase7-worker-" \
    '[.[] | select(.hostname | startswith($prefix))]'


if [ "$PHASE7_IDLE_COUNT" -ge "$WORKER_COUNT" ]; then
    pass "$WORKER_COUNT workers returned/remained IDLE"
else
    fail "Expected $WORKER_COUNT IDLE workers after test, found $PHASE7_IDLE_COUNT"
fi


# ----------------------------------------------------
# Concurrency summary
# ----------------------------------------------------

log "Concurrency verification"

echo
echo "Test configuration:"
echo "  Workers:              $WORKER_COUNT"
echo "  RPS per worker:       $RPS"
echo "  Concurrency per worker: $CONCURRENCY"
echo
echo "Expected fleet-wide concurrency ceiling:"
echo "  $WORKER_COUNT × $CONCURRENCY = $((WORKER_COUNT * CONCURRENCY))"
echo
echo "The execution-layer semaphore is responsible for enforcing"
echo "the per-worker concurrency ceiling."
echo "Test 9 code-level verification established this guarantee."


# ----------------------------------------------------
# Final
# ----------------------------------------------------

log "Phase 7 integration checks finished"

echo
echo "Test ID: $TEST_ID"
echo "Workers: $WORKER_COUNT"
echo "Concurrency/worker: $CONCURRENCY"
echo "RPS/worker: $RPS"
echo "Duration: ${TEST_DURATION}s"