#!/bin/bash

set -e

echo ""
echo "======================================================="
echo "        Vulcan Phase 5 Integration Test"
echo "======================================================="
echo ""

SERVER="http://localhost:8080"
VICTORIA="http://localhost:8428"

echo "[1] Creating test..."

TEST_RESPONSE=$(
curl -s \
-X POST \
"$SERVER/api/v1/tests" \
-H "Content-Type: application/json" \
-d '{
    "name":"Phase6 Container Test",
    "worker_count":3,
    "target_url":"https://httpbin.org/get",
    "method":"GET",
    "duration_sec":30,
    "rps":100,
    "concurrency":20
}'
)

echo "$TEST_RESPONSE"

TEST_ID=$(echo "$TEST_RESPONSE" | jq -r '.id')

echo ""
echo "Test ID: $TEST_ID"

echo ""
echo "[2] Waiting for workers..."
sleep 2

echo ""
echo "[3] Starting test..."

curl -s \
-X POST \
"$SERVER/api/v1/tests/$TEST_ID/start"

echo ""
echo ""
echo "[4] Waiting for workers to publish metrics..."

FOUND=false

for i in {1..30}
do
    RESULT=$(curl -s \
        "$VICTORIA/api/v1/export?match[]=vulcan_requests_total")

    if echo "$RESULT" | grep -q "$TEST_ID"; then
        FOUND=true
        break
    fi

    sleep 1
done

echo ""

if [ "$FOUND" = false ]; then
    echo "ERROR: Metrics for test $TEST_ID were not found."
    exit 1
fi

echo "Metrics detected."

echo ""
echo "[5] Exported Metrics"
echo ""

curl -s "$VICTORIA/api/v1/export?match[]=vulcan_requests_total"

echo ""
echo ""

curl -s "$VICTORIA/api/v1/export?match[]=vulcan_success_total"

echo ""
echo ""

curl -s "$VICTORIA/api/v1/export?match[]=vulcan_failure_total"

echo ""
echo ""

curl -s "$VICTORIA/api/v1/export?match[]=vulcan_avg_latency_ms"

echo ""
echo ""

curl -s "$VICTORIA/api/v1/export?match[]=vulcan_workers"

echo ""
echo ""
echo "======================================================="
echo "Phase 5 Integration Test Passed"
echo "======================================================="