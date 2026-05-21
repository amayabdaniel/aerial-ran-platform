#!/usr/bin/env bash
# demo.sh — full end-to-end walkthrough of aerial-ran-platform.
# Assumes: `make up` has been run, the 7 Go services are running on host
# ports 8081-8087, and the k3d cluster is up with Open5GS + MongoDB.
#
# Usage:  ./scripts/demo.sh
set -eu

BASE="${BASE:-http://localhost:18080}"
EMAIL="${EMAIL:-demo-$(date +%s)@aerial.local}"
PW="correct-horse-battery-staple"

green()  { printf "\033[32m%s\033[0m\n" "$*"; }
yellow() { printf "\033[33m%s\033[0m\n" "$*"; }
header() { echo; printf "\033[36m── %s ──\033[0m\n" "$*"; }

require() { command -v "$1" >/dev/null || { echo "need $1 in PATH"; exit 1; }; }
require curl
require python3
require docker

j() { python3 -m json.tool; }
pick() { python3 -c "import sys,json; d=json.load(sys.stdin); $1"; }

header "0  gateway health"
curl -fsS "$BASE/healthz"

header "1  sign up new user (or login if exists)"
SIGNUP=$(curl -sS -X POST "$BASE/api/iam/v1/auth/signup" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PW\",\"display_name\":\"Demo\",\"org_name\":\"Demo Org\",\"org_slug\":\"demo-$(date +%s)\"}" \
  -w '\n%{http_code}')
CODE=$(echo "$SIGNUP" | tail -1)
yellow "  signup status=$CODE"

TP=$(curl -sS -X POST "$BASE/api/iam/v1/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PW\",\"device_fingerprint\":\"demo-cli\"}")
TOKEN=$(echo "$TP" | pick "print(d['access_token'])")
USER_ID=$(echo "$TP" | pick "print(d['user']['id'])")
ORG_ID=$(echo "$TP" | pick "print(d['user']['org_id'])")
green "  user_id=$USER_ID  org_id=$ORG_ID"
H="Authorization: Bearer $TOKEN"

header "2  subscribe to Aerial Family plan"
curl -sS -X POST "$BASE/api/provision/v1/subscriptions" -H "$H" -H 'Content-Type: application/json' \
  -d '{"plan_id":"aerial-family"}' | j | head -10 || true

header "3  issue a SIM (Ki/OPc auto-generated, pushed to Open5GS MongoDB)"
SIM=$(curl -sS -X POST "$BASE/api/subscriber/v1/sims" -H "$H" -H 'Content-Type: application/json' -d '{}')
IMSI=$(echo "$SIM" | pick "print(d['imsi'])")
SIM_ID=$(echo "$SIM" | pick "print(d['id'])")
green "  → IMSI $IMSI  sim_id $SIM_ID"
echo "  → confirm in Open5GS mongo (counts subscribers globally):"
docker exec aerial-postgres true 2>/dev/null && {
  yellow "  (skip mongo verify — would require k8s exec, run: kubectl -n ran exec deploy/open5gs-mongodb -- mongosh --quiet open5gs --eval 'db.subscribers.countDocuments()')"
}

header "4  order a Colombia 5GB eSIM (mock provider)"
ORDER=$(curl -sS -X POST "$BASE/api/esim/v1/esims" -H "$H" -H 'Content-Type: application/json' -d '{"package_id":"mock-CO-5g-30d"}')
ESIM_ID=$(echo "$ORDER" | pick "print(d['id'])")
ICCID=$(echo "$ORDER" | pick "print(d.get('iccid','?'))")
LPA=$(echo "$ORDER" | pick "print(d.get('lpa_string','?'))")
QR_BYTES=$(echo "$ORDER" | pick "print(len(d.get('qr_png_b64','')))")
green "  → ICCID $ICCID"
green "  → LPA   $LPA"
green "  → QR PNG (base64) $QR_BYTES bytes"

header "5  poll usage (mock simulates usage growth)"
for i in 1 2 3; do
  U=$(curl -sS -X POST "$BASE/api/esim/v1/esims/$ESIM_ID/usage/refresh" -H "$H" | pick "print(d['last_usage_mb'])")
  yellow "  poll $i → used $U MB"
done

header "6  ingest a manual billing event"
curl -sS -X POST "$BASE/api/billing/v1/usage/events" -H "$H" -H 'Content-Type: application/json' \
  -d "{\"source\":\"manual\",\"data_mb\":200,\"minutes\":5,\"sms_count\":2,\"cents\":75,\"occurred_at\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}" \
  -o /dev/null -w "  status=%{http_code}\n"
USAGE=$(curl -sS "$BASE/api/billing/v1/usage" -H "$H")
green "  $(echo $USAGE | pick "print('data='+str(d['data_mb'])+'MB voice='+str(d['minutes'])+'min sms='+str(d['sms_count'])+' cost=\$'+format(d['cents']/100,'.2f'))")"

header "7  send a self-message via NATS-JetStream"
curl -sS -X POST "$BASE/api/messaging/v1/messages" -H "$H" -H 'Content-Type: application/json' \
  -d "{\"to_user_id\":\"$USER_ID\",\"body\":\"hola desde demo.sh 🛰\"}" | pick "print('  → msg_id', d['id'])"
INBOX=$(curl -sS "$BASE/api/messaging/v1/messages/inbox" -H "$H")
echo "$INBOX" | pick "[print('  -',m['body']) for m in d[:3]]"

header "8  RAN status (Open5GS NFs + MongoDB subscriber count)"
curl -sS "$BASE/api/ran/v1/ran/status" -H "$H" | pick "print(f\"  PLMN={d['plmn']}  subscribers={d['subscribers']}  scrape={d['scrape_duration_ms']}ms\")"

header "9  list IMSIs in Open5GS"
curl -sS "$BASE/api/ran/v1/ran/subscribers" -H "$H" | pick "[print('  -',i) for i in d]"

green ""
green "=========================================="
green "  DEMO COMPLETE"
green "=========================================="
echo
echo "Open the web UI:   open $BASE/ui/"
echo "Sign in as:        $EMAIL  /  $PW"
echo
