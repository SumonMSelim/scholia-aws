#!/usr/bin/env bash
# Fails when total statement coverage in a Go cover profile is below a minimum.
# Usage: scripts/coverage-gate.sh coverage.out 85
set -euo pipefail

profile="${1:?coverage profile required}"
min="${2:?minimum percentage required}"

total="$(go tool cover -func="$profile" | awk '/^total:/ {gsub("%", "", $3); print $3}')"
if [[ -z "$total" ]]; then
  echo "coverage: could not read total from $profile" >&2
  exit 1
fi

if awk -v t="$total" -v m="$min" 'BEGIN { exit !(t + 0 < m + 0) }'; then
  echo "coverage: ${total}% is below the ${min}% gate" >&2
  exit 1
fi
echo "coverage: ${total}% (gate ${min}%)"
