#!/usr/bin/env bash
# Prod smoke test (Sprint 10 capstone launch gate).
#
# Curls the live Core API and the live web app and asserts on status codes
# (and, where noted, response body content). Prints PASS/FAIL per check and
# exits non-zero if anything failed, so it can gate a publish/rollback step.
#
# Usage:
#   scripts/smoke.sh [BASE] [WEB]
#   BASE defaults to https://backend.switeaz.com
#   WEB  defaults to https://switeaz.com
set -u

BASE="${1:-https://backend.switeaz.com}"
WEB="${2:-https://switeaz.com}"

FAILED=0

# check <name> <url> <expected_status> [body_substring]
check() {
	local name="$1" url="$2" expected="$3" want_body="${4:-}"
	local body_file
	body_file="$(mktemp)"
	local code
	code="$(curl -s -o "$body_file" -w '%{http_code}' --max-time 15 "$url")"

	local ok=1
	if [ "$code" != "$expected" ]; then
		ok=0
	fi
	if [ -n "$want_body" ] && ! grep -q -- "$want_body" "$body_file"; then
		ok=0
	fi

	if [ "$ok" -eq 1 ]; then
		echo "PASS [$code] $name ($url)"
	else
		echo "FAIL [$code, want $expected] $name ($url)"
		if [ -n "$want_body" ]; then
			echo "     expected body to contain: $want_body"
		fi
		FAILED=1
	fi
	rm -f "$body_file"
}

echo "== TradeBridge prod smoke =="
echo "BASE=$BASE"
echo "WEB=$WEB"
echo

# --- Core API ---
check "health"               "$BASE/health"                                200 "ok"
check "metrics"               "$BASE/metrics"                              200 "http_requests_total"
check "account export guarded" "$BASE/v1/account/export"                   401
check "public discovery"       "$BASE/public/v1/discovery?q=test"          200

# --- Web app ---
check "web home"       "$WEB/"              200
check "web pricing"    "$WEB/pricing"       200
check "web legal terms" "$WEB/legal/terms"  200
check "web robots.txt"  "$WEB/robots.txt"   200

echo
if [ "$FAILED" -eq 0 ]; then
	echo "ALL CHECKS PASSED"
	exit 0
else
	echo "ONE OR MORE CHECKS FAILED"
	exit 1
fi
