#!/usr/bin/env bash
# seed-family.sh — provision 5 family accounts end-to-end.
# Creates: 5 users (all under the "amaya-family" org), each with a SIM provisioned
# into Open5GS MongoDB, an eSIM ordered against the mock provider, and a
# subscription to the Aerial Family plan.
#
# Usage:  ./scripts/seed-family.sh                        # uses default base http://localhost:18080
#         BASE=http://192.168.1.5:18080 ./scripts/seed-family.sh
set -e

BASE="${BASE:-http://localhost:18080}"
PW="family-demo-2026"
ORG_SLUG="amaya-family-$(date +%s)"
ORG_NAME="Amaya Family"

# name:email
FAMILY=(
  "Daniel:daniel.family@aerial.local"
  "Ana:ana.family@aerial.local"
  "Sofía:sofia.family@aerial.local"
  "Mateo:mateo.family@aerial.local"
  "Abuela:abuela.family@aerial.local"
)

green()  { printf "\033[32m%s\033[0m\n" "$*"; }
yellow() { printf "\033[33m%s\033[0m\n" "$*"; }
cyan()   { printf "\033[36m%s\033[0m\n" "$*"; }
header() { echo; printf "\033[36m── %s ──\033[0m\n" "$*"; }

pick() { python3 -c "import sys,json; d=json.load(sys.stdin); $1"; }

# Ensure mock catalog is loaded (idempotent — the demo user is fine to do this).
header "0. preflight"
curl -fsS "$BASE/healthz" >/dev/null
green "  gateway ok"

# Use the first family member as the org admin / catalog refresher.
ADMIN_EMAIL="${FAMILY[0]##*:}"
ADMIN_NAME="${FAMILY[0]%%:*}"

# Sign up (ignore conflict).
curl -sS -X POST "$BASE/api/iam/v1/auth/signup" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$PW\",\"display_name\":\"$ADMIN_NAME\",\"org_name\":\"$ORG_NAME\",\"org_slug\":\"$ORG_SLUG\"}" \
  -o /dev/null -w '  admin signup: status=%{http_code}\n' || true

# Login.
TP=$(curl -sS -X POST "$BASE/api/iam/v1/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$PW\"}")
ADMIN_TOK=$(echo "$TP" | pick "print(d['access_token'])")
ADMIN_ID=$(echo "$TP" | pick "print(d['user']['id'])")
ORG_ID=$(echo "$TP" | pick "print(d['user']['org_id'])")
green "  admin=$ADMIN_NAME org_id=$ORG_ID"
H_ADMIN="Authorization: Bearer $ADMIN_TOK"

# Refresh catalog for Colombia + US so kids can pick.
curl -fsS -X POST "$BASE/api/esim/v1/catalog/refresh?region=CO" -H "$H_ADMIN" -o /dev/null
curl -fsS -X POST "$BASE/api/esim/v1/catalog/refresh?region=US" -H "$H_ADMIN" -o /dev/null
green "  catalog refreshed (CO + US)"

# admin already has an account; provision their SIM + eSIM + subscription too
provision_member() {
  local name=$1 email=$2 tok=$3 first_member=$4

  curl -sS -X POST "$BASE/api/provision/v1/subscriptions" -H "Authorization: Bearer $tok" -H 'Content-Type: application/json' \
    -d '{"plan_id":"aerial-family"}' -o /dev/null -w "    subscribed: status=%{http_code}\n" || true

  SIM=$(curl -sS -X POST "$BASE/api/subscriber/v1/sims" -H "Authorization: Bearer $tok" -H 'Content-Type: application/json' -d '{}')
  IMSI=$(echo "$SIM" | pick "print(d['imsi'])")
  yellow "    SIM:  IMSI=$IMSI"

  ESIM=$(curl -sS -X POST "$BASE/api/esim/v1/esims" -H "Authorization: Bearer $tok" -H 'Content-Type: application/json' \
    -d '{"package_id":"mock-CO-5g-30d"}')
  ICCID=$(echo "$ESIM" | pick "print(d.get('iccid','?'))")
  yellow "    eSIM: ICCID=$ICCID  (mock provider)"
}

# Provision admin first.
header "1. provisioning ${ADMIN_NAME} (admin)"
provision_member "$ADMIN_NAME" "$ADMIN_EMAIL" "$ADMIN_TOK" true

# Add the rest.
for entry in "${FAMILY[@]:1}"; do
  NAME="${entry%%:*}"
  EMAIL="${entry##*:}"
  header "2. provisioning $NAME"
  # Sign up (org is auto-created for them too because we don't have a "invite to org" flow yet —
  # for v1 each user gets their own org; for v2 we should add invite tokens).
  curl -sS -X POST "$BASE/api/iam/v1/auth/signup" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$EMAIL\",\"password\":\"$PW\",\"display_name\":\"$NAME\"}" \
    -o /dev/null -w "    signup: status=%{http_code}\n" || true
  TP=$(curl -sS -X POST "$BASE/api/iam/v1/auth/login" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$EMAIL\",\"password\":\"$PW\"}")
  TOK=$(echo "$TP" | pick "print(d['access_token'])")
  USER_ID=$(echo "$TP" | pick "print(d['user']['id'])")
  green "    user_id=$USER_ID"
  # Refresh their own catalog since they're in their own org.
  curl -fsS -X POST "$BASE/api/esim/v1/catalog/refresh?region=CO" -H "Authorization: Bearer $TOK" -o /dev/null || true
  provision_member "$NAME" "$EMAIL" "$TOK" false
done

header "3. summary"
green "  5 family members provisioned"
green "  password (everyone): $PW"
echo
echo "  Try logging in as:"
for entry in "${FAMILY[@]}"; do
  echo "    ${entry##*:}"
done
echo
echo "  Web UI: $BASE/ui/"
echo "  RAN subscribers (Open5GS):"
curl -fsS "$BASE/api/ran/v1/ran/subscribers" -H "$H_ADMIN" | pick "[print('    -',i) for i in d]" || true
