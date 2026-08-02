#!/usr/bin/env bash

set -euo pipefail

##############################################
# Vulcan Phase 6 Integration Test
##############################################

CONTROL_PLANE="http://localhost:8080"
VICTORIA="http://localhost:8428"
GRAFANA="http://localhost:3000"

PASS() {
    echo "$1"
}

FAIL() {
    echo "$1"
    exit 1
}

INFO() {
    echo
    echo "----------------------------------------------------"
    echo "$1"
    echo "----------------------------------------------------"
}

##############################################
# Health Checks
##############################################

INFO "Checking Control Plane"

curl -sf ${CONTROL_PLANE}/health >/dev/null \
&& PASS "Control Plane reachable" \
|| FAIL "Control Plane unavailable"

INFO "Checking VictoriaMetrics"

curl -sf ${VICTORIA}/health >/dev/null \
&& PASS "VictoriaMetrics reachable" \
|| FAIL "VictoriaMetrics unavailable"

INFO "Checking Grafana"

curl -sf ${GRAFANA}/login >/dev/null \
&& PASS "Grafana reachable" \
|| FAIL "Grafana unavailable"

##############################################
# Create Test
##############################################

INFO "Creating test"

TEST_RESPONSE=$(
curl -s \
-X POST \
${CONTROL_PLANE}/api/v1/tests \
-H "Content-Type: application/json" \
-d '{
    "name":"Phase6 Integration Test",
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

[ "$TEST_ID" != "null" ] && [ -n "$TEST_ID" ] \
&& PASS "Created test: $TEST_ID" \
|| FAIL "Unable to create test"

##############################################
# Start Test
##############################################

INFO "Starting test"

curl -sf \
    -X POST \
    ${CONTROL_PLANE}/api/v1/tests/${TEST_ID}/start >/dev/null

PASS "Test started"

##############################################
# Waiting
##############################################

INFO "Waiting for workers to publish metrics"

sleep 12

##############################################
# Live Metrics Endpoint
##############################################

INFO "Testing Live Metrics API"

LIVE=$(curl -s \
${CONTROL_PLANE}/api/v1/tests/${TEST_ID}/metrics)

echo "$LIVE"

REQUESTS=$(echo "$LIVE" | jq '.requests')
WORKERS=$(echo "$LIVE" | jq '.workers')

if [ "$REQUESTS" -gt 0 ] && [ "$WORKERS" -gt 0 ]; then
    PASS "Live metrics endpoint returned telemetry"
else
    FAIL "Live metrics endpoint returned empty metrics"
fi
##############################################
# History Endpoint
##############################################

INFO "Testing History Endpoint"

NOW=$(date +%s)
START=$((NOW-300))

HISTORY=$(curl -s \
"${CONTROL_PLANE}/api/v1/tests/${TEST_ID}/metrics/history?start=${START}&end=${NOW}&step=10")

echo "$HISTORY"

echo "$HISTORY" | grep "\"series\"" >/dev/null \
&& PASS "History endpoint works" \
|| FAIL "History endpoint failed"

##############################################
# VictoriaMetrics Query
##############################################

INFO "Testing VictoriaMetrics"

QUERY=$(curl -sG \
    "${VICTORIA}/api/v1/query" \
    --data-urlencode "query=vulcan_requests_total{test_id=\"${TEST_ID}\"}" \
    --data-urlencode "latency_offset=1ms")

echo "$QUERY"

RESULT_COUNT=$(echo "$QUERY" | jq '.data.result | length')

[ "$RESULT_COUNT" -gt 0 ] \
&& PASS "VictoriaMetrics contains telemetry" \
|| FAIL "VictoriaMetrics missing telemetry"

##############################################
# Grafana Datasource
##############################################

INFO "Checking Grafana Datasource"

curl -sf \
-u admin:admin \
${GRAFANA}/api/datasources >/dev/null \
&& PASS "Datasource API reachable" \
|| FAIL "Datasource unavailable"

##############################################
# Dashboard Provisioning
##############################################

INFO "Checking Dashboard"

curl -sf \
-u admin:admin \
${GRAFANA}/api/search \
| grep "Vulcan" >/dev/null \
&& PASS "Dashboard provisioned" \
|| FAIL "Dashboard not provisioned"

##############################################
# Summary
##############################################

echo
echo "==============================================="
echo "PHASE 6 TESTS PASSED"
echo "==============================================="
echo
echo "Validated:"
echo "- Control Plane"
echo "- VictoriaMetrics"
echo "- Grafana"
echo "- Test Creation"
echo "- Test Start"
echo "- Live Metrics API"
echo "- History API"
echo "- VictoriaMetrics Queries"
echo "- Datasource Provisioning"
echo "- Dashboard Provisioning"
echo
echo "Phase 6 Observability Layer: COMPLETE"