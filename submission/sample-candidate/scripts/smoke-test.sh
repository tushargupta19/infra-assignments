#!/usr/bin/env bash
# End-to-end smoke test for config-service.
#
# Starts a background kubectl port-forward, then validates:
#   1. /ping responds with "pong"          (liveness)
#   2. /readyz responds 200                (readiness / DB connectivity)
#   3. POST /configs creates a record
#   4. GET  /configs/:id returns the record just created
#   5. GET  /configs/:id for an unknown id returns 404
#
# Usage: scripts/smoke-test.sh <kube-context> <namespace> <app-name>
set -euo pipefail

KUBE_CONTEXT="${1:-kind-config-service}"
NAMESPACE="${2:-config-service}"
APP_NAME="${3:-config-service}"
LOCAL_PORT="${SMOKE_TEST_PORT:-18080}"
BASE_URL="http://127.0.0.1:${LOCAL_PORT}"

echo "==> starting port-forward on :${LOCAL_PORT}"
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
  port-forward "svc/${APP_NAME}" "${LOCAL_PORT}:8080" >/tmp/config-service-pf.log 2>&1 &
PF_PID=$!

cleanup() {
  kill "${PF_PID}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "==> waiting for port-forward to become ready"
for _ in $(seq 1 30); do
  if curl -s -o /dev/null "${BASE_URL}/ping"; then
    break
  fi
  sleep 1
done

echo "==> GET /ping"
body="$(curl -sf "${BASE_URL}/ping")"
if [[ "${body}" != "pong" ]]; then
  echo "FAIL: expected 'pong', got '${body}'" >&2
  exit 1
fi
echo "OK: /ping -> pong"

echo "==> GET /readyz"
status="$(curl -s -o /dev/null -w '%{http_code}' "${BASE_URL}/readyz")"
if [[ "${status}" != "200" ]]; then
  echo "FAIL: /readyz returned ${status}, expected 200" >&2
  exit 1
fi
echo "OK: /readyz -> 200"

ID="smoke-test-$(date +%s)"
echo "==> POST /configs (id=${ID})"
status="$(curl -s -o /dev/null -w '%{http_code}' -X POST "${BASE_URL}/configs" \
  -H 'Content-Type: application/json' \
  -d "{\"id\":\"${ID}\",\"host\":\"localhost\",\"port\":8080,\"app_name\":\"smoke-test\",\"log_level\":\"INFO\"}")"
if [[ "${status}" != "200" ]]; then
  echo "FAIL: POST /configs returned ${status}, expected 200" >&2
  exit 1
fi
echo "OK: POST /configs -> 200"

echo "==> GET /configs/${ID}"
resp="$(curl -sf "${BASE_URL}/configs/${ID}")"
if [[ "${resp}" != *"\"id\":\"${ID}\""* ]]; then
  echo "FAIL: unexpected response body: ${resp}" >&2
  exit 1
fi
echo "OK: GET /configs/${ID} -> ${resp}"

echo "==> GET /configs/does-not-exist (expect 404)"
status="$(curl -s -o /dev/null -w '%{http_code}' "${BASE_URL}/configs/does-not-exist")"
if [[ "${status}" != "404" ]]; then
  echo "FAIL: expected 404, got ${status}" >&2
  exit 1
fi
echo "OK: unknown id -> 404"

echo ""
echo "All smoke tests passed."
