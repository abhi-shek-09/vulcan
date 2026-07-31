#!/bin/bash

set -e

echo ""
echo "======================================================="
echo "        Vulcan Phase 5 Integration Test"
echo "======================================================="
echo ""

SERVER="http://localhost:8080"

echo "[1] Creating test..."

TEST_RESPONSE=$(
curl -s \
-X POST \
"$SERVER/api/v1/tests" \
-H "Content-Type: application/json" \
-d '{
    "name":"Phase5 Integration Test",
    "worker_count":3
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
echo "[4] Waiting for workers to execute..."
sleep 12

echo ""
echo "[5] Querying VictoriaMetrics..."

echo ""
echo "Requests"

curl -s \
"http://localhost:8428/api/v1/query?query=vulcan_requests_total"

echo ""
echo ""
echo "Success"

curl -s \
"http://localhost:8428/api/v1/query?query=vulcan_success_total"

echo ""
echo ""
echo "Failures"

curl -s \
"http://localhost:8428/api/v1/query?query=vulcan_failure_total"

echo ""
echo ""
echo "Latency"

curl -s \
"http://localhost:8428/api/v1/query?query=vulcan_avg_latency_ms"

echo ""
echo ""
echo "Workers"

curl -s \
"http://localhost:8428/api/v1/query?query=vulcan_workers"

echo ""
echo ""
echo "======================================================="
echo "Pipeline Verification Complete"
echo "======================================================="