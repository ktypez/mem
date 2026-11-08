#!/usr/bin/env bash
# Smoke-check mem health, auth, writer trust policy, and probe cleanup.
# Never prints the API token or contents of the environment file.
set -euo pipefail

BASE=${BASE:-http://127.0.0.1:3200}
ENV_FILE=${ENV_FILE:-/etc/mem/mem.env}
READER=${READER:-sudo -n}

read_env() {
  local key=$1
  if [[ "$READER" == "" ]]; then
    python3 - "$ENV_FILE" "$key" <<'PY'
import sys
for line in open(sys.argv[1], encoding="utf-8"):
    if line.startswith(sys.argv[2] + "="):
        print(line.split("=", 1)[1].strip().strip("\"'") )
        break
PY
  else
    $READER python3 - "$ENV_FILE" "$key" <<'PY'
import sys
for line in open(sys.argv[1], encoding="utf-8"):
    if line.startswith(sys.argv[2] + "="):
        print(line.split("=", 1)[1].strip().strip("\"'") )
        break
PY
  fi
}

TOKEN=${MEM_API_TOKEN:-$(read_env MEM_API_TOKEN)}
[[ -n "$TOKEN" ]] || { printf 'MEM_API_TOKEN not found (value suppressed)\n' >&2; exit 1; }
AUTH="Authorization: Bearer $TOKEN"
TRUSTED=${MEM_STEWARD_SOURCE:-$(read_env MEM_STEWARD_SOURCE || true)}
unset TOKEN

health=$(curl -fsS -o /dev/null -w '%{http_code}' "$BASE/health")
no_auth=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/api/brief")
printf 'health HTTP: %s (expect 200)\nunauthenticated API HTTP: %s (expect 401 when auth configured)\n' "$health" "$no_auth"
[[ "$health" == 200 ]] || exit 1
[[ "$no_auth" == 401 ]] || { printf 'API auth gate did not return 401\n' >&2; exit 1; }
printf 'trusted writers: %s\n' "${TRUSTED:-<default> }"

created=()
cleanup() {
  for id in "${created[@]}"; do
    curl -fsS -H "$AUTH" -X DELETE "$BASE/v1/memory/nodes/$id" >/dev/null || true
  done
}
trap cleanup EXIT

for source in steward hermes agent; do
  response=$(curl -fsS -X POST "$BASE/v1/memory/nodes" \
    -H "$AUTH" -H "X-Mem-Source: $source" -H 'Content-Type: application/json' \
    -d "{\"body\":\"mem gate verification probe $source\",\"topic\":\"mem-gate-verify-$$\"}")
  id=$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("id", ""))' <<<"$response")
  status=$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("status", ""))' <<<"$response")
  [[ -n "$id" ]] || { printf 'Probe create failed for %s\n' "$source" >&2; exit 1; }
  created+=("$id")
  printf '%s => %s\n' "$source" "$status"
done

stats=$(curl -fsS -H "$AUTH" "$BASE/v1/memory/stats")
python3 -c 'import json,sys; d=json.load(sys.stdin); print("stats:", json.dumps(d, ensure_ascii=False, sort_keys=True))' <<<"$stats"
cleanup
created=()
printf 'probe rows deleted\n'
