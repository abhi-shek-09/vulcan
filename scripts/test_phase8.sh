#!/bin/bash
set -u

BASE_URL="${BASE_URL:-http://localhost:8080}"
TARGET_URL="${TARGET_URL:-http://localhost:9090}"
RPS="${RPS:-350}"
CAPACITY="${WORKER_CAPACITY_RPS:-100}"
DURATION="${TEST_DURATION:-10}"
CONCURRENCY="${CONCURRENCY:-4}"

PASS=0
FAIL=0
TEST_ID=""

pass() { echo "PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "FAIL: $1"; FAIL=$((FAIL + 1)); }

echo "Checking control plane..."
if curl -fsS "$BASE_URL/api/v1/tests" >/dev/null; then
    pass "Control Plane reachable"
else
    fail "Control Plane not reachable"
    exit 1
fi

echo "Creating test without worker_count..."
CREATE_RESPONSE="$(
    curl -fsS         -X POST "$BASE_URL/api/v1/tests"         -H "Content-Type: application/json"         -d "{
            \"name\": \"phase8-integration\",
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

if [ "$REQUIRED" = "4" ] && [ "$RPS" = "350" ]; then
    pass "Required workers calculated as 4"
else
    fail "Expected 4 workers, got $REQUIRED"
    exit 1
fi

echo "Starting test..."
START_RESPONSE="$(
    curl -fsS         -X POST "$BASE_URL/api/v1/tests/$TEST_ID/start"
)"

echo "$START_RESPONSE" | jq .

ASSIGNED="$(echo "$START_RESPONSE" | jq '.workers | length')"

if [ "$ASSIGNED" = "4" ]; then
    pass "4 workers assigned"
else
    fail "Expected 4 assigned workers, got $ASSIGNED"
    exit 1
fi

STATUS="$(curl -fsS "$BASE_URL/api/v1/tests/$TEST_ID" | jq -r '.status')"
if [ "$STATUS" = "RUNNING" ]; then
    pass "Test entered RUNNING state"
else
    fail "Expected RUNNING, got $STATUS"
fi

# Verify the persisted per-worker RPS allocation directly in Postgres.
# This avoids adding a Phase 8 API solely for testing internal assignment data.
if command -v docker >/dev/null 2>&1; then
    DB_ROWS="$(
        docker exec vulcan-postgres         psql -U "${POSTGRES_USER:-vulcandev}"              -d "${POSTGRES_DB:-vulcandb}"              -Atc "SELECT rps FROM test_workers WHERE test_id = '$TEST_ID' ORDER BY rps DESC;"         2>/dev/null || true
    )"

    echo "Worker RPS allocations:"
    echo "$DB_ROWS"

    TOTAL=0
    COUNT=0
    while IFS= read -r value; do
        [ -z "$value" ] && continue
        TOTAL=$((TOTAL + value))
        COUNT=$((COUNT + 1))
    done <<< "$DB_ROWS"

    if [ "$COUNT" = "4" ] && [ "$TOTAL" = "$RPS" ]; then
        pass "Worker RPS allocations sum to $RPS"
    else
        fail "Expected 4 allocations summing to $RPS; got count=$COUNT total=$TOTAL"
    fi
else
    echo "SKIP: docker unavailable; cannot inspect persisted worker RPS"
fi

echo "Waiting for test completion..."
DEADLINE=$((SECONDS + DURATION + 20))
FINAL_STATUS="$STATUS"

while [ "$SECONDS" -lt "$DEADLINE" ]; do
    FINAL_STATUS="$(curl -fsS "$BASE_URL/api/v1/tests/$TEST_ID" | jq -r '.status')"
    if [ "$FINAL_STATUS" = "COMPLETED" ] || [ "$FINAL_STATUS" = "FAILED" ]; then
        break
    fi
    sleep 1
done

if [ "$FINAL_STATUS" = "COMPLETED" ]; then
    pass "Test completed normally"
else
    fail "Expected COMPLETED, got $FINAL_STATUS"
fi

echo
echo "Phase 8 Passed: $PASS"
echo "Phase 8 Failed: $FAIL"

if [ "$FAIL" -gt 0 ]; then
    echo "PHASE 8: FAILED"
    exit 1
fi

echo "PHASE 8: PASSED"
