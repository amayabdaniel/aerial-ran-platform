#!/usr/bin/env bash
# Start / stop the 7 host-bound aerial services.
# Each service writes a PID to /tmp/aerial-<name>.pid and logs to /tmp/aerial-<name>.log.
# Pure bash 3.2-compatible (no associative arrays — macOS ships old bash).
set -e

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$ROOT/bin"
JWT="${JWT_SECRET:-dev-secret-change-in-production-32ch}"
DB_BASE="${DATABASE_URL_BASE:-postgres://aerial_admin:aerial_dev_pass_change_me@localhost:15432/aerial?sslmode=disable}"
MONGO="${OPEN5GS_MONGO_URI:-mongodb://localhost:27017}"
NATS_URL="${NATS_URL:-nats://localhost:14222}"

# svc:port:schema
SERVICES=(
  "svc-aerial-iam:8081:iam"
  "svc-aerial-subscriber:8082:subscriber"
  "svc-aerial-esim:8083:esim"
  "svc-aerial-provision:8084:provision"
  "svc-aerial-ran-control:8085:ranctl"
  "svc-aerial-billing:8086:billing"
  "svc-aerial-messaging:8087:messaging"
)

start_one() {
  local svc=$1 port=$2 schema=$3
  local pidfile=/tmp/aerial-$svc.pid log=/tmp/aerial-$svc.log
  if [ -f "$pidfile" ] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then
    echo "running $svc (pid $(cat "$pidfile")) — skip"
    return
  fi
  env \
    PORT="$port" \
    DATABASE_URL="${DB_BASE}&search_path=${schema}" \
    JWT_SECRET="$JWT" \
    OPEN5GS_MONGO_URI="$MONGO" \
    NATS_URL="$NATS_URL" \
    "$BIN/$svc" >"$log" 2>&1 &
  echo $! >"$pidfile"
  echo "started $svc on :$port pid=$!"
}

stop_one() {
  local svc=$1
  local pidfile=/tmp/aerial-$svc.pid
  if [ -f "$pidfile" ]; then
    local pid; pid=$(cat "$pidfile")
    if kill -0 "$pid" 2>/dev/null; then kill "$pid" 2>/dev/null || true; fi
    rm -f "$pidfile"
    echo "stopped $svc"
  fi
}

cmd=${1:-start}
case "$cmd" in
  start)
    for entry in "${SERVICES[@]}"; do
      IFS=':' read -r svc port schema <<<"$entry"
      start_one "$svc" "$port" "$schema"
    done
    echo
    echo "=== waiting for all services to be healthy ==="
    for entry in "${SERVICES[@]}"; do
      IFS=':' read -r svc port schema <<<"$entry"
      for i in $(seq 1 20); do
        code=$(curl -s -o /dev/null -w '%{http_code}' "http://localhost:$port/v1/health" 2>/dev/null || true)
        if [ "$code" = "200" ]; then printf "  :%-4s %s OK\n" "$port" "$svc"; break; fi
        sleep 0.3
        if [ "$i" = "20" ]; then printf "  :%-4s %s DOWN (last=%s)\n" "$port" "$svc" "$code"; fi
      done
    done
    ;;
  stop)
    for entry in "${SERVICES[@]}"; do
      IFS=':' read -r svc port schema <<<"$entry"
      stop_one "$svc"
    done
    ;;
  status)
    for entry in "${SERVICES[@]}"; do
      IFS=':' read -r svc port schema <<<"$entry"
      pidfile=/tmp/aerial-$svc.pid
      if [ -f "$pidfile" ] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then
        echo "$svc UP pid=$(cat "$pidfile") port=$port"
      else
        echo "$svc DOWN"
      fi
    done
    ;;
  *) echo "usage: $0 {start|stop|status}"; exit 2;;
esac
