#!/usr/bin/env bash
# Walks the whole platform end to end through the gateway, in the order the spec's
# interaction flows describe: a request is created and joined, a seller offers against
# the aggregated demand, an admin approves it, and only then can the seller see a phone
# number. Every call goes to :8080 - if something here needs a service port, the
# gateway is wrong.
#
#   docker compose up --build          # in another terminal, wait for it to settle
#   ./scripts/smoke.sh
#
# BASE=http://localhost:8080 overrides the target. Needs curl and python3, nothing else.

set -uo pipefail

BASE="${BASE:-http://localhost:8080}"
RUN="$(date +%s)"
PASS=0; FAIL=0

bold=$'\e[1m'; red=$'\e[31m'; grn=$'\e[32m'; dim=$'\e[2m'; off=$'\e[0m'
step() { printf '\n%s== %s%s\n' "$bold" "$1" "$off"; }
ok()   { PASS=$((PASS+1)); printf '  %s✓%s %s\n' "$grn" "$off" "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  %s✗%s %s\n' "$red" "$off" "$1"; [ -n "${2:-}" ] && printf '    %s%s%s\n' "$dim" "$2" "$off"; }

# api METHOD PATH [BODY] [TOKEN] -> sets STATUS and BODY
api() {
  local method="$1" path="$2" body="${3:-}" token="${4:-}"
  local args=(-s -o /tmp/smoke.body -w '%{http_code}' -X "$method" "$BASE$path")
  [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
  [ -n "$body" ]  && args+=(-H 'Content-Type: application/json' -d "$body")
  STATUS="$(curl "${args[@]}")"
  BODY="$(cat /tmp/smoke.body)"
}

# expect EXPECTED_STATUS LABEL
expect() {
  if [ "$STATUS" = "$1" ]; then ok "$2 ${dim}($STATUS)${off}"
  else bad "$2 — expected $1, got $STATUS" "$(printf '%s' "$BODY" | head -c 200)"; fi
}

# field KEY -> prints the value of a top-level key, empty if absent
field() { printf '%s' "$BODY" | python3 -c "
import json,sys
try: print(json.load(sys.stdin).get('$1',''))
except Exception: print('')
"; }

# count -> number of items, whether the payload is a bare list or wraps one
count() { printf '%s' "$BODY" | python3 -c "
import json,sys
try:
    d=json.load(sys.stdin)
    if isinstance(d,list): print(len(d))
    else:
        for k in ('items','content','notifications','data','results'):
            if isinstance(d.get(k),list): print(len(d[k])); break
        else: print(0)
except Exception: print(0)
"; }

# types -> the distinct notification types in a list payload, one per line
types() { printf '%s' "$BODY" | python3 -c "
import json,sys
try:
    d=json.load(sys.stdin)
    rows = d if isinstance(d,list) else next((d[k] for k in ('items','content','notifications','data','results') if isinstance(d.get(k),list)), [])
    for t in sorted({r.get('type','?') for r in rows}): print(t)
except Exception: pass
"; }

# ---------------------------------------------------------------------------------

step "Gateway is up"
api GET /health
expect 200 "GET /health"

step "Customer registers, logs in and creates a profile"
api POST /api/auth/register/customer "{\"email\":\"c1-$RUN@test.com\",\"password\":\"password123\",\"phoneNumber\":\"+2161$RUN\"}"
expect 201 "register customer 1"
api POST /api/auth/login "{\"email\":\"c1-$RUN@test.com\",\"password\":\"password123\"}"
expect 200 "login customer 1"
C1="$(field accessToken)"
[ -n "$C1" ] && ok "got an access token" || bad "no accessToken in the login response"
api POST /api/customers '{"firstName":"Amine","lastName":"Hmida"}' "$C1"
expect 201 "create customer profile"

step "Spec flow 1 — create a request"
api POST /api/requests '{"itemName":"Espresso Machine","description":"bar grade","category":"kitchen","quantity":3}' "$C1"
expect 201 "create request"
REQ="$(field requestId)"
[ -n "$REQ" ] && ok "requestId $REQ" || { bad "no requestId — cannot continue"; exit 1; }

step "Spec flow 1 — a second customer joins it"
api POST /api/auth/register/customer "{\"email\":\"c2-$RUN@test.com\",\"password\":\"password123\",\"phoneNumber\":\"+2162$RUN\"}"
expect 201 "register customer 2"
api POST /api/auth/login "{\"email\":\"c2-$RUN@test.com\",\"password\":\"password123\"}"
expect 200 "login customer 2"
C2="$(field accessToken)"
api POST /api/customers '{"firstName":"Second","lastName":"Customer"}' "$C2"
expect 201 "create customer 2 profile"
api POST "/api/requests/$REQ/participants" '{"quantity":5}' "$C2"
expect 201 "customer 2 joins with quantity 5"
api GET "/api/requests/$REQ" "" "$C1"
expect 200 "read the request back"
printf '    %saggregated demand: %s%s\n' "$dim" "$(printf '%s' "$BODY" | head -c 300)" "$off"

step "Spec flow 2 — a seller offers against the aggregated demand"
api POST /api/auth/register/seller "{\"email\":\"s1-$RUN@test.com\",\"password\":\"password123\",\"phoneNumber\":\"+2163$RUN\"}"
expect 201 "register seller"
api POST /api/auth/login "{\"email\":\"s1-$RUN@test.com\",\"password\":\"password123\"}"
expect 200 "login seller"
S1="$(field accessToken)"
api POST /api/sellers '{"storeName":"Amine Store","description":"Electronics","city":"Tunis","address":"12 Rue Example"}' "$S1"
expect 201 "create seller profile"
api POST /api/offers "{\"requestId\":\"$REQ\",\"availableQuantity\":6,\"pricePerUnit\":\"149.99\",\"currency\":\"EUR\",\"description\":\"Sealed, 24 month warranty\"}" "$S1"
expect 201 "create offer"
OFFER="$(field offerId)"
[ -n "$OFFER" ] && ok "offerId $OFFER" || bad "no offerId"

step "The seller cannot see contacts yet"
api GET "/api/contacts/requests/$REQ" "" "$S1"
expect 403 "contacts refused before approval"

step "Spec flow 3 — the admin approves"
api POST /api/auth/login '{"email":"admin@marketplace.local","password":"admin-dev-password"}'
expect 200 "login the seeded admin"
AD="$(field accessToken)"
api GET /api/admin/offers/pending "" "$AD"
expect 200 "list pending offers"
printf '    %spending: %s offer(s)%s\n' "$dim" "$(count)" "$off"
api POST "/api/admin/offers/$OFFER/approve" '{"reason":"Best price for the aggregated demand"}' "$AD"
# 201, not 200: the approval creates a decision record, and the response carries its
# decisionId rather than the mutated offer.
expect 201 "approve the offer"

step "Only now is the phone number reachable"
api GET "/api/contacts/requests/$REQ" "" "$S1"
expect 200 "contacts granted after approval"
printf '    %s%s%s\n' "$dim" "$(printf '%s' "$BODY" | head -c 300)" "$off"
if printf '%s' "$BODY" | grep -q '+216'; then ok "a phone number came through, and only here"
else bad "no phone number in the contacts response"; fi

step "Notifications arrived over RabbitMQ (outbox relay, so allow a moment)"
for i in $(seq 15); do
  api GET /api/notifications/me "" "$S1"
  [ "$(count)" -gt 0 ] && break
  sleep 1
done
expect 200 "seller reads their notifications"
printf '    %sseller: %s notification(s): %s%s\n' "$dim" "$(count)" "$(types | paste -sd, -)" "$off"
if [ "$(count)" -gt 0 ]; then ok "the event reached notification-service"
else bad "no notifications after 15s — check the relay and the broker"; fi
api GET /api/notifications/me "" "$C2"
printf '    %scustomer 2: %s notification(s): %s%s\n' "$dim" "$(count)" "$(types | paste -sd, -)" "$off"
api GET /api/notifications/me/unread-count "" "$S1"
expect 200 "unread count"

step "Closing a request fans out to every participant"
api POST /api/requests '{"itemName":"Cafetiere","description":"to be closed","category":"kitchen","quantity":1}' "$C1"
expect 201 "create a second request"
CLOSE="$(field requestId)"
api POST "/api/requests/$CLOSE/participants" '{"quantity":2}' "$C2"
expect 201 "customer 2 joins it"
api POST "/api/requests/$CLOSE/close" "" "$C1"
expect 200 "owner closes it"
api POST "/api/requests/$CLOSE/close" "" "$C1"
expect 409 "closing twice is refused"

step "The gateway boundary (spec section 6)"
api GET "/internal/requests/$REQ"
expect 404 "/internal has no route"
api GET /actuator/health
expect 404 "/actuator has no route"
api GET /api/requestsfoo
expect 404 "prefix confusion does not route"
api GET /api/requests/me
expect 401 "no token is rejected"

step "CORS preflight"
CH="$(curl -s -o /dev/null -D - -X OPTIONS "$BASE/api/requests" \
      -H 'Origin: http://localhost:5173' -H 'Access-Control-Request-Method: POST' \
      -H 'Access-Control-Request-Headers: authorization,content-type' | tr -d '\r')"
if printf '%s' "$CH" | grep -qi 'access-control-allow-origin: http://localhost:5173'; then
  ok "preflight from an allowed origin is answered at the edge"
else bad "preflight did not return the origin" "$(printf '%s' "$CH" | head -3)"; fi
CH="$(curl -s -o /dev/null -D - -X OPTIONS "$BASE/api/requests" -H 'Origin: http://evil.com' \
      -H 'Access-Control-Request-Method: POST' | tr -d '\r')"
if printf '%s' "$CH" | grep -qi 'access-control-allow-origin'; then
  bad "an unlisted origin was allowed"
else ok "an unlisted origin gets no allow-origin header"; fi

printf '\n%s%d passed, %d failed%s\n' "$bold" "$PASS" "$FAIL" "$off"
[ "$FAIL" -eq 0 ] || exit 1
