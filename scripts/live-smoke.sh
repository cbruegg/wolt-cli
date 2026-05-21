#!/usr/bin/env bash
# live-smoke.sh — exercise read-only wolt-cli commands against the live
# upstream. Detects Wolt-side contract drift before users do.
#
# Invoked weekly by .github/workflows/live-smoke.yml. Also runnable
# locally: just run it, it'll use whatever ~/.wolt/.wolt-config.json
# you're already logged into.
#
# In CI, set:
#   WOLT_SMOKE_CONFIG_JSON  contents of ~/.wolt/.wolt-config.json
# When set, the script writes it to ~/.wolt/.wolt-config.json verbatim,
# overwriting any existing local login. Seed it with:
#     gh secret set WOLT_SMOKE_CONFIG_JSON < ~/.wolt/.wolt-config.json
# That carries the full set of session cookies Wolt requires
# (telemetryDeviceId, activeLocation, etc.) — synthesising only
# __wtoken/__wrtoken returns 401 "session expired".
# Skip the env-var path when running locally to keep your real session intact.
#
# READ-ONLY ENDPOINTS ONLY. Never add login/logout, cart-add/remove/
# clear, or checkout placement — this script runs on a real account.

set -euo pipefail

readonly WOLT_BIN="${WOLT_BIN:-./bin/wolt}"
# Central Helsinki — Rautatientori. Hardcoded so the smoke surface is
# stable across runs; the venue catalogue and feed shape vary by city.
readonly HEL_LAT="60.1699"
readonly HEL_LON="24.9384"
readonly KNOWN_VENUE="${WOLT_SMOKE_VENUE:-burger-king-finnoo}"
readonly SMOKE_DIR="${SMOKE_DIR:-${TMPDIR:-/tmp}/wolt-smoke}"

mkdir -p "${SMOKE_DIR}"

pass=0
fail=0
declare -a failures=()

# redact — strip anything resembling a user identifier from a stream so
# stderr can be safely printed to the public Actions log. We err on the
# side of redacting too much; debugging always has the local file with
# the unredacted body available outside the Actions log.
redact() {
  sed -E \
    -e 's/eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/<JWT>/g' \
    -e 's/[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}/<EMAIL>/g' \
    -e 's/[a-f0-9]{24}/<OID>/g' \
    -e 's/(__wtoken|__wrtoken|wtoken|wrtoken|wrefresh_token|access_token|refresh_token)=[^"&;[:space:]]+/\1=<REDACTED>/gI' \
    -e 's/Bearer [A-Za-z0-9._-]+/Bearer <REDACTED>/gI' \
    -e 's/-?[0-9]{1,3}\.[0-9]{3,}, ?-?[0-9]{1,3}\.[0-9]{3,}/<LATLON>/g'
}

# run "label" cmd args...
# Captures stdout JSON to ${SMOKE_DIR}/<label>.json. Stderr to
# ${SMOKE_DIR}/<label>.err. On non-zero exit, prints a REDACTED stderr
# tail AND the redacted error envelope (code+message), since wolt-cli
# in --format json puts errors in stdout via emitError.
run() {
  local label="$1"; shift
  local slug="${label// /_}"
  local out="${SMOKE_DIR}/${slug}.json"
  local err="${SMOKE_DIR}/${slug}.err"
  printf "[%s] %-22s ... " "$(date -u +%H:%M:%S)" "${label}"
  if "$@" --format json >"${out}" 2>"${err}"; then
    printf "ok (%s bytes)\n" "$(wc -c <"${out}" | tr -d ' ')"
    pass=$((pass + 1))
  else
    local rc=$?
    printf "FAIL (exit %d)\n" "${rc}"
    {
      # Surface the envelope error first — it's the canonical reason
      # wolt-cli exited non-zero in JSON mode.
      jq -r '
        .errors // empty
        | "code:    \(.code // "-")\nmessage: \(.message // "-")"
      ' "${out}" 2>/dev/null
      # Then any stderr (verbose trace, panics, etc.) for completeness.
      head -10 "${err}" 2>/dev/null
    } | redact | sed 's/^/    | /' | head -20 || true
    fail=$((fail + 1))
    failures+=("${label}")
  fi
}

# seed_config_from_env — when CI hands us the full config blob, write
# it to ~/.wolt/.wolt-config.json with owner-only perms. Roundtrip
# through jq so we (a) validate it's well-formed JSON before disk
# touch, (b) overwrite the local location with Helsinki center for
# stable smoke results.
seed_config_from_env() {
  if [ -z "${WOLT_SMOKE_CONFIG_JSON:-}" ]; then
    return 0
  fi
  mkdir -p "${HOME}/.wolt"
  umask 077
  printf '%s' "${WOLT_SMOKE_CONFIG_JSON}" \
    | jq --argjson lat "${HEL_LAT}" --argjson lon "${HEL_LON}" \
        '.account.location = {lat: $lat, lon: $lon}' \
    >"${HOME}/.wolt/.wolt-config.json"
  chmod 600 "${HOME}/.wolt/.wolt-config.json"
}

seed_config_from_env

# Pre-flight: print a redacted HTTP trace of the first authenticated
# call so the public log shows the actual status code Wolt returned
# (instead of the smoke just printing "FAIL"). Verbose lines look like
# "[http] -> GET <url>" / "[http] <- GET <url> status=N duration=Yms" —
# the redactor scrubs any token/ID/email that sneaks in.
echo "-- pre-flight diagnostic --"
"${WOLT_BIN}" status --format json --verbose >/dev/null 2>"${SMOKE_DIR}/preflight.stderr" || true
grep -E '^\[(http|verbose)\]' "${SMOKE_DIR}/preflight.stderr" 2>/dev/null \
  | redact \
  | sed 's/^/    /' \
  | head -10 || true
echo "-- end pre-flight --"

# ---- read-only smoke surface --------------------------------------

# status doubles as the auth-refresh exerciser — if Wolt's refresh
# contract drifted, this is where it shows up first.
run "status"            "${WOLT_BIN}" status
run "account"           "${WOLT_BIN}" account
run "account orders"    "${WOLT_BIN}" account orders --limit 3
run "account payments"  "${WOLT_BIN}" account payments
run "account addresses" "${WOLT_BIN}" account addresses
run "account favorites" "${WOLT_BIN}" account favorites --limit 5

# Chase one order detail — this is the endpoint whose 429 behavior we
# rely on in stats. Skipped when the account has no orders.
if order_id="$(jq -r '.data.orders[0].purchase_id // .data.orders[0]._id // ""' "${SMOKE_DIR}/account_orders.json" 2>/dev/null)" && [ -n "${order_id}" ]; then
  run "account order"   "${WOLT_BIN}" account order "${order_id}"
else
  printf "[%s] %-22s ... skipped (no orders to drill into)\n" "$(date -u +%H:%M:%S)" "account order"
fi

run "feed summary"  "${WOLT_BIN}" feed --summary
run "top 5"         "${WOLT_BIN}" top 5
run "venues query"  "${WOLT_BIN}" venues --query burger --limit 3
run "venue static"  "${WOLT_BIN}" venue "${KNOWN_VENUE}"
run "venue menu"    "${WOLT_BIN}" venue menu "${KNOWN_VENUE}"
run "cart"          "${WOLT_BIN}" cart
run "cart count"    "${WOLT_BIN}" cart count

# ---- summary -------------------------------------------------------

echo ""
echo "== summary =="
echo "passed: ${pass}"
echo "failed: ${fail}"
if [ "${fail}" -gt 0 ]; then
  printf "failed steps: %s\n" "$(IFS=', '; echo "${failures[*]}")"
  exit 1
fi
