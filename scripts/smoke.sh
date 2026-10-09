#!/usr/bin/env bash
# Verifies a deployed Scholia instance end to end, the way a judge would use it:
# web app, client-side routing, API health, a guest session, a public course, a
# new chat and one streamed answer. SMOKE_COURSE picks the course; otherwise the
# first public course is used. Retries while CloudFront propagates.
#
# Usage: scripts/smoke.sh https://example.cloudfront.net
# Each run starts one guest session and spends one guest message.
set -euo pipefail

base="${1:?base URL required}"
base="${base%/}"
attempts="${SMOKE_ATTEMPTS:-30}"

check() {
  local name="$1" path="$2" expect="$3" body status
  for ((i = 1; i <= attempts; i++)); do
    body="$(curl -sS --max-time 15 -w '\n%{http_code}' "${base}${path}" || true)"
    status="${body##*$'\n'}"
    body="${body%$'\n'*}"
    if [[ "$status" == "200" && "$body" == *"$expect"* ]]; then
      echo "ok   ${name}"
      return 0
    fi
    sleep 10
  done
  echo "FAIL ${name}: status ${status}, expected body to contain ${expect}" >&2
  return 1
}

# post sends a JSON body with the SHA-256 header CloudFront origin access control requires.
post() {
  local path="$1" body="$2" token="${3:-}" hash
  hash="$(printf '%s' "$body" | shasum -a 256 | cut -d' ' -f1)"
  local args=(-sS --max-time 60 -X POST "${base}${path}" -H 'Content-Type: application/json' -H "x-amz-content-sha256: ${hash}")
  if [[ -n "$token" ]]; then
    args+=(-H "X-Scholia-Token: ${token}")
  fi
  curl "${args[@]}" --data-binary "$body"
}

field() {
  sed -nE "s/.*\"$1\":\"([^\"]*)\".*/\\1/p" | head -n1
}

fail() {
  echo "FAIL $1" >&2
  exit 1
}

check "web app"            "/"          "<title>Scholia"
check "client-side route"  "/chat"      "<title>Scholia"
check "api health"         "/api/health" '"status":"ok"'

token="$(post /api/session/guest '' | field token)"
[[ -n "$token" ]] || fail "guest session: no token"
echo "ok   guest session"

# Usage needs a session, so this fails if the edge drops or rewrites the token header.
usage="$(curl -sS --max-time 15 "${base}/api/account/usage" -H "X-Scholia-Token: ${token}")"
[[ "$usage" == *'"guest":true'* ]] || fail "guest token not accepted: ${usage:0:300}"
echo "ok   guest token accepted"

courses="$(curl -sS --max-time 15 "${base}/api/courses" -H "X-Scholia-Token: ${token}")"
course="${SMOKE_COURSE:-$(grep -oE '\{"id":"[^"]+"[^{}]*"public":true' <<<"$courses" | head -n1 | field id || true)}"
[[ -n "$course" && "$courses" == *"\"id\":\"${course}\""* ]] || fail "no public course in ${courses:0:300}"
echo "ok   public course listed (${course})"

chat="$(post /api/chats "{\"course_id\":\"${course}\",\"model\":\"\"}" "$token" | field id)"
[[ -n "$chat" ]] || fail "create chat: no id"
echo "ok   chat created"

reply="$(post "/api/chats/${chat}/messages" '{"question":"What does this course cover?","mode":"answer"}' "$token")"
if [[ "$reply" != *"event: delta"* && "$reply" != *"event: refusal"* ]]; then
  fail "stream: unexpected reply ${reply:0:300}"
fi
if [[ "$reply" == *"event: error"* ]]; then
  fail "stream: error event ${reply:0:300}"
fi
echo "ok   streamed answer"
