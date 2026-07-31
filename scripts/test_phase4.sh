#!/bin/bash

set -e

BASE_URL="http://localhost:8080/api/v1"

echo "======================================"
echo " Vulcan Phase 4 Integration Test"
echo "======================================"

#########################################
# Register Worker
#########################################

echo ""
echo "Registering worker..."

WORKER_RESPONSE=$(curl -s -X POST \
    "$BASE_URL/workers" \
    -H "Content-Type: application/json" \
    -d '{
        "hostname":"worker-test-3",
        "version":"v1.0.0",
        "cpu_count":8,
        "memory_mb":16384
    }')

echo "$WORKER_RESPONSE"

WORKER_ID=$(echo "$WORKER_RESPONSE" | jq -r '.id')

echo ""
echo "Worker ID = $WORKER_ID"

#########################################
# Create Test
#########################################

echo ""
echo "Creating test..."

TEST_RESPONSE=$(curl -s -X POST \
    "$BASE_URL/tests" \
    -H "Content-Type: application/json" \
    -d '{
        "name":"Phase 4 Test-1.2",
        "worker_count":1
    }')

echo "$TEST_RESPONSE"

TEST_ID=$(echo "$TEST_RESPONSE" | jq -r '.ID')

echo ""
echo "Test ID = $TEST_ID"

#########################################
# Start Test
#########################################

echo ""
echo "Starting test..."

curl -s -X POST \
"$BASE_URL/tests/$TEST_ID/start"

echo ""
echo "Started."

#########################################
# Poll Assignment
#########################################

echo ""
echo "Checking assignment..."

curl -s \
"$BASE_URL/workers/$WORKER_ID/assignment"

echo ""

#########################################
# Start Assignment
#########################################

echo ""
echo "Mark assignment RUNNING..."

curl -s -X POST \
"$BASE_URL/workers/$WORKER_ID/assignment/start" \
-H "Content-Type: application/json" \
-d "{
    \"test_id\":\"$TEST_ID\"
}"

echo ""
echo "Done."

#########################################
# Verify
#########################################

echo ""
echo "Current Assignment"

curl -s \
"$BASE_URL/workers/$WORKER_ID/assignment"

echo ""

#########################################
# Complete
#########################################

echo ""
echo "Completing assignment..."

curl -s -X POST \
"$BASE_URL/workers/$WORKER_ID/assignment/complete" \
-H "Content-Type: application/json" \
-d "{
    \"test_id\":\"$TEST_ID\"
}"

echo ""
echo "Completed."

#########################################
# Verify Again
#########################################

echo ""
echo "Assignment after completion"

curl -s \
"$BASE_URL/workers/$WORKER_ID/assignment"

echo ""

#########################################
# Stop Test
#########################################

echo ""
echo "Stopping test..."

curl -s -X POST \
"$BASE_URL/tests/$TEST_ID/stop"

echo ""

echo "======================================"
echo " Phase 4 Test Finished"
echo "======================================"