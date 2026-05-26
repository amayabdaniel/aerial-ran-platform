#!/usr/bin/env bash
# Persistent Open5GS MongoDB port-forward (127.0.0.1:27017) for host-bound services.
# Restarts itself on failure. Use:
#   ./scripts/mongo-pf.sh start    # launch in the background, survives this shell
#   ./scripts/mongo-pf.sh stop
#   ./scripts/mongo-pf.sh status
set -e

PIDFILE=/tmp/aerial-mongo-pf.pid
LOGFILE=/tmp/aerial-mongo-pf.log

start() {
  if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
    echo "already running pid=$(cat "$PIDFILE")"
    return
  fi
  # Loop the port-forward so it auto-restarts if it dies.
  nohup bash -c '
    while true; do
      kubectl -n ran port-forward svc/open5gs-mongodb 27017:27017
      echo "[$(date)] port-forward exited; restarting in 2s" >&2
      sleep 2
    done
  ' >"$LOGFILE" 2>&1 &
  echo $! > "$PIDFILE"
  disown
  sleep 1
  echo "started pid=$(cat "$PIDFILE") (log: $LOGFILE)"
}

stop() {
  if [ -f "$PIDFILE" ]; then
    pid=$(cat "$PIDFILE")
    # kill the supervisor loop + any kubectl child
    pkill -P "$pid" 2>/dev/null || true
    kill "$pid" 2>/dev/null || true
    rm -f "$PIDFILE"
    echo "stopped"
  fi
  # nuke any orphan kubectl port-forwards too
  pkill -f 'port-forward.*open5gs-mongodb' 2>/dev/null || true
}

status() {
  if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
    echo "supervisor UP pid=$(cat "$PIDFILE")"
    pgrep -fl 'kubectl.*port-forward.*open5gs-mongodb' || echo "  (no kubectl yet)"
  else
    echo "DOWN"
  fi
}

case "${1:-start}" in
  start) start ;;
  stop) stop ;;
  status) status ;;
  *) echo "usage: $0 {start|stop|status}"; exit 2;;
esac
