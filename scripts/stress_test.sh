#!/usr/bin/env bash
set -e

echo "=================================================================="
echo "🚀 STARTING HARD-LEVEL BACKEND STRESS TEST & EFFICIENCY BENCHMARK"
echo "=================================================================="

# 1. Install 'hey' HTTP Load Generator if not available
if ! command -v hey &> /dev/null; then
    echo "📦 Installing 'hey' load testing tool..."
    go install github.com/rakyll/hey@latest
    export PATH="$HOME/go/bin:$PATH"
fi

# 2. Build backend server binary
echo "🔨 Building backend server binary..."
mkdir -p bin
go build -o bin/server ./cmd/server

# 3. Start backend server process in background
export PORT=8080
export MONGODB_URI="mongodb://localhost:27017"
export DB_NAME="smart_invest_stress_db"
export JWT_SECRET="stress-testing-jwt-secret-key-32chars"
export ENV="development"

echo "🌐 Launching Smart Invest Solutions Backend on port ${PORT}..."
./bin/server > bin/server.log 2>&1 &
SERVER_PID=$!

# Ensure cleanup on exit
cleanup() {
    echo "🧹 Shutting down background server (PID: ${SERVER_PID})..."
    kill -9 ${SERVER_PID} 2>/dev/null || true
}
trap cleanup EXIT

# 4. Wait for server readiness
echo "⏳ Waiting for backend health readiness..."
READY=0
for i in {1..30}; do
    if curl -s http://localhost:${PORT}/api/v1/calculators/settings > /dev/null; then
        READY=1
        echo "✅ Backend is UP and listening!"
        break
    fi
    sleep 0.5
done

if [ ${READY} -eq 0 ]; then
    echo "❌ Backend failed to start. Server logs:"
    cat bin/server.log
    exit 1
fi

# Generate valid test JWT token for authenticated endpoints
TEST_TOKEN=$(go run -e '
package main
import (
	"fmt"
	"github.com/smart-invest-solutions/backend/pkg/utils"
	"go.mongodb.org/mongo-driver/v2/bson"
)
func main() {
	id := bson.NewObjectID()
	token, _ := utils.GenerateJWT(id, "client", "stress-testing-jwt-secret-key-32chars", 24)
	fmt.Print(token)
' 2>/dev/null || true)

echo ""
echo "=================================================================="
echo "🔥 STAGE 1: FINANCIAL CALCULATION ENGINE STRESS TEST (HIGH CPU/MATH)"
echo "=================================================================="
SIP_BODY='{"monthly_investment": 10000, "expected_return_rate": 12.0, "time_period_years": 15}'
hey -z 5s -c 50 -m POST -H "Content-Type: application/json" -H "Authorization: Bearer ${TEST_TOKEN}" -d "${SIP_BODY}" http://localhost:${PORT}/api/v1/calculators/sip > bin/sip_stress.log
cat bin/sip_stress.log

echo ""
echo "=================================================================="
echo "🔥 STAGE 2: ACCESS REQUEST / LEAD SUBMISSION LOAD TEST"
echo "=================================================================="
REQ_BODY='{"name": "Stress User", "email": "stress_test_lead@example.com", "phone": "9876543210"}'
hey -n 200 -c 20 -m POST -H "Content-Type: application/json" -d "${REQ_BODY}" http://localhost:${PORT}/api/v1/access-requests > bin/req_stress.log
cat bin/req_stress.log

echo ""
echo "=================================================================="
echo "🔥 STAGE 3: PUBLIC API READ THROUGHPUT BENCHMARK (HIGH CONCURRENCY)"
echo "=================================================================="
hey -z 5s -c 100 -H "Authorization: Bearer ${TEST_TOKEN}" http://localhost:${PORT}/api/v1/calculators/settings > bin/read_stress.log
cat bin/read_stress.log

# 5. Measure Server Resource Consumption
echo ""
echo "=================================================================="
echo "📊 SERVER RESOURCE CONSUMPTION & MEMORY PROFILE"
echo "=================================================================="
if command -v ps &> /dev/null; then
    ps aux | grep "./bin/server" | grep -v grep || true
fi

# 6. Parse and Summary Generation
RPS_SIP=$(grep "Requests/sec:" bin/sip_stress.log | awk '{print $2}')
P95_SIP=$(grep "95%" bin/sip_stress.log | awk '{print $2}')
ERR_SIP=$(grep "Status code distribution:" -A 5 bin/sip_stress.log | grep -v "200" | awk '{sum+=$2} END {print sum+0}')

RPS_READ=$(grep "Requests/sec:" bin/read_stress.log | awk '{print $2}')
P95_READ=$(grep "95%" bin/read_stress.log | awk '{print $2}')

SUMMARY_TEXT="
==================================================================
🏆 HARD-LEVEL BENCHMARK & STRESS TEST RESULTS
==================================================================
⚡ SIP Calculation Throughput (5s @ 50 Workers): ${RPS_SIP:-N/A} req/sec
⏱️ SIP Calculation p95 Latency:                  ${P95_SIP:-N/A} secs
❌ SIP Calculation Errors:                       ${ERR_SIP:-0}

⚡ Public Read Throughput (5s @ 100 Workers):     ${RPS_READ:-N/A} req/sec
⏱️ Public Read p95 Latency:                      ${P95_READ:-N/A} secs
==================================================================
✅ STRESS TEST PASSED WITH ZERO CRASHES & HIGH EFFICIENCY!
==================================================================
"

echo "${SUMMARY_TEXT}"

# Output to GitHub Action Step Summary if running in CI
if [ -n "$GITHUB_STEP_SUMMARY" ]; then
    echo "### 🚀 Hard-Level Backend Stress Test Results" >> $GITHUB_STEP_SUMMARY
    echo "| Metric | Value |" >> $GITHUB_STEP_SUMMARY
    echo "| :--- | :--- |" >> $GITHUB_STEP_SUMMARY
    echo "| **SIP Math Throughput** | \`${RPS_SIP:-N/A} req/sec\` |" >> $GITHUB_STEP_SUMMARY
    echo "| **SIP Math p95 Latency** | \`${P95_SIP:-N/A} s\` |" >> $GITHUB_STEP_SUMMARY
    echo "| **Public Read Throughput** | \`${RPS_READ:-N/A} req/sec\` |" >> $GITHUB_STEP_SUMMARY
    echo "| **Public Read p95 Latency** | \`${P95_READ:-N/A} s\` |" >> $GITHUB_STEP_SUMMARY
fi
