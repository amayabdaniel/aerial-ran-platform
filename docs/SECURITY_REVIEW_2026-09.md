# Security review — 2026-09-19

Attack-surface enumeration across eight vectors for the aerial-ran-platform
control plane (7 Go services + `lib-aerial-go`, Postgres/pgx, NATS, Open5GS
Mongo, k3d/k8s). Each vector carries an exposure verdict; fixed items cite the
commit, deliberately-unfixed items say why. Companion to `HARDENING_LOG.md`.

## ⚠ Most severe finding — a published JWT signing key that a default deployment actually uses

This is not "a weak default waiting for a missing env var." The HS256 signing key
`dev-secret-change-in-production-32ch` is **public in this repository and a
cluster deployed from it as-is runs on that key by default**:

- `infra/k8s/platform/00-infra.yaml:15` ships the literal as the **actual value**
  of the `aerial-secrets` Secret's `JWT_SECRET` key.
- `infra/k8s/platform/20-services.yaml` wires every service's `JWT_SECRET` via
  `secretKeyRef: {name: aerial-secrets, key: JWT_SECRET}` — so the reference
  **resolves to the committed key**; nothing is unset, nothing fails closed.
- All **7 services** additionally hardcode the same string as their in-code
  fallback (billing, esim, iam, messaging, provision, ran-control, subscriber),
  and validation only checks `len >= 16` — the string is exactly 32 chars, so it
  passes. 11 tracked files at HEAD contain the literal.

**Consequence, stated plainly: any token forged with this public string is
accepted today, on every authenticated endpoint, for any `org` and any `role`.**
An attacker who has read the repo can mint an admin token for any tenant. There
is no exploitation precondition beyond reachability.

Correct fix (not shipped this pass — it spans code + ops + manifests and cannot be
validated without a running deployment; half-wiring a fail-closed guard across 7
services would break dev boot, which is worse than the documented state):
1. **Rotate the key out of git** into a real secret store; treat the current value
   as compromised.
2. Add `jwt.CheckSecret` rejecting empty / `< 32` / the known-placeholder value
   unless `ALLOW_INSECURE_JWT_SECRET=true`, wired into **every** entrypoint
   (`runner.Run` + the iam/subscriber/esim custom mains).
3. Set the dev opt-in in the committed dev manifests so `make up`/k8s still boot.

Escalated to Daniel as the most severe single finding of this pass.

## Companion finding — committed Postgres credential (same class, in-cluster reach)

The database password is the **same class** as the JWT secret — a committed
credential a default deployment actually uses, not a dev fallback a real deploy
overrides:

- `00-infra.yaml:14` ships `aerial_dev_pass_change_me` as the actual value of
  `aerial-secrets`'s `POSTGRES_PASSWORD`, and **the Postgres container is
  initialised with it** (`secretKeyRef` at `00-infra.yaml:101`) — so the DB is
  *created* with this password.
- All 7 per-service DSNs embed it inline as the `DATABASE_URL_*` Secret values
  (`00-infra.yaml:17-23`), and `20-services.yaml` resolves each service's
  `DATABASE_URL` from those keys — a default deploy connects with the committed
  password. 12 tracked files contain it at HEAD.

**Severity: high, one notch below the JWT secret — the distinction is reach, and
it is real, not a downgrade of the class.** Postgres is a `ClusterIP` service
(not externally exposed), so using the credential directly needs in-cluster
network access, whereas the JWT key is exploitable through the public gateway on
any authenticated endpoint. Compounding factor: every DSN sets `sslmode=disable`,
so the password and all DB traffic are plaintext on the pod network — any
in-cluster sniffer sees the credential regardless. Same remediation shape as the
JWT secret (rotate out of git as compromised → real secret store; enable TLS),
and it is documented here rather than rotated in this pass for the same reason.

## Summary

| # | Vector | Verdict | Action |
|---|--------|---------|--------|
| 1 | AuthN/AuthZ | **EXPOSED** | **FIXED** `880b94c` — cross-tenant IDOR closed |
| 2 | Injection | not exposed | none needed (parameterized/constant) |
| 3 | Transport & secrets | **EXPOSED (critical)** | CSWSH **FIXED** `986e29e`; published JWT key + DB credential still with Daniel (see top) |
| 4 | Input handling & DoS | **EXPOSED** | **FIXED** `09c657e` — 4 MiB body limit |
| 5 | Supply chain | **EXPOSED** | **FIXED** `9726451` — x/text→v0.39.0; digest pinning documented |
| 6 | Data exposure | **EXPOSED** | **FIXED** `67aab62` — DBError no longer leaks driver text |
| 7 | Concurrency & state | not exposed (see below) | none needed |
| 8 | Infra & config | **EXPOSED** | **PARTIALLY FIXED** `4180069` — 7 Go workloads hardened; infra images + NATS auth follow-up |

## 1 — AuthN/AuthZ — FIXED (`880b94c`)

**Cross-tenant IDOR on every by-id endpoint.** `create`/`list` scoped queries by
`claims.OrgID`, but the by-id siblings passed the path id straight to the service
with no org check: subscriber `get/suspend/resume/terminate`, esim
`get/usage-refresh/activate/cancel`, provision `cancel`. Any authenticated user
in any tenant could read another org's SIM (IMSI/MSISDN/APN), **deprovision their
line from the 5G core** (cross-tenant DoS), or cancel their subscriptions.
Fixed: each by-id op now enforces the caller's org and returns not-found across
tenants (no existence oracle). Subscriber has a failing-pre-fix ownership test;
esim/provision carry the same guard. `Ki`/`OPc` were already `json:"-"` so no key
material was ever returned.

## 2 — Injection — NOT EXPOSED

All SQL uses pgx `$N` placeholders; the one dynamically-assembled query
(`svc-aerial-esim/.../postgres.go`) appends only static text + a `$1` placeholder.
Mongo filters are constant `bson.M{}` (no request data). No `os/exec`, no
`http.ServeFile`/path traversal on request input. httplog writes via slog's JSON
handler, which escapes control chars, so log injection is not reachable.

## 3 — Transport & secrets — EXPOSED (documented, not patched this pass)

- **Published JWT signing key** — the most severe finding of the pass; see the
  dedicated section at the top of this document for the 7-service + committed-
  Secret breakdown and the fix. In short: a default deployment runs on a key that
  is public in the repo, and any token forged with it is accepted today.
- **Committed Postgres credential** — same class as the JWT secret (committed and
  actually used by a default deploy), high severity, in-cluster reach. See the
  dedicated companion section near the top for the breakdown; `sslmode=disable`
  makes the credential and all DB traffic plaintext on the pod network.
- **WebSocket CSWSH — FIXED (`986e29e`)**: `svc-aerial-messaging` set
  `InsecureSkipVerify: true` on `websocket.Accept`, disabling Origin checking
  (cross-site WebSocket hijack if the service is reached directly). Replaced with
  `OriginPatterns` derived from `ALLOWED_ORIGINS` (via `OriginHostsFromCSV`): the
  library now enforces same-origin plus the allow-list and rejects everything else
  with 403, independent of the JWT check. The WS-upgrade path IS unit-testable
  after all — the Origin check runs before the hijack, so a `httptest` handshake
  with a hostile Origin gets the 403 (failing-pre-fix test added).

## 4 — Input handling & DoS — FIXED (`09c657e`)

No body-size limit existed: every handler `json.Decode(r.Body)` with no cap, so a
single connection could force unbounded buffering (memory DoS). Added a 4 MiB
`MaxBytesReader` middleware to the shared `runner` chain; oversized reads surface
as 400. **Still open (documented):** no rate-limiting middleware exists despite
`CLAUDE.md` advertising "100 req/sec per IP" — the documented chain (secure
headers, ratelimit, timeout, body_limit) is largely aspirational; only
recover→metrics→cors→httplog→auth is wired. A few list queries lack `LIMIT`
(`ListPlans`, `ListPackages`, `OrgMonth`, Mongo subscriber list) but are bounded
by catalog/rollup cardinality, not attacker input — low severity.

## 5 — Supply chain — FIXED (`9726451`, partial)

Bumped `golang.org/x/text` v0.29.0–v0.37.0 → **v0.39.0** across all modules,
closing **GO-2026-5970**. **Still open (documented):** no container image is
digest-pinned; base images float within their tag and app images use a mutable
`:dev` tag — recommend `@sha256` pinning. Go toolchain is pinned (`go 1.26.1`) and
all `replace` directives are in-repo. Fourteen stdlib vulnerabilities exist in the
installed go1.26.1 toolchain (4× crypto/x509, 3× crypto/tls, 3× net/http, plus
net/url, net, net/textproto, encoding/asn1), fixed in go1.26.6 — a **machine-level
toolchain upgrade, Daniel's call**, deliberately not attempted here.

## 6 — Data exposure — FIXED (`67aab62`)

`respond.DBError`'s 500 path echoed the raw driver/internal error to the client,
leaking schema (table/column/constraint names), SQL fragments and host detail —
reconnaissance. Now logged server-side; client gets a generic message. See the
`HARDENING_LOG.md` entry for the companion 22P02→400 mapping.

## 7 — Concurrency & state — NOT EXPOSED

Health uses an `atomic.Bool` updated on a ticker (no per-request race). No
check-then-act TOCTOU on security decisions: the IDOR fix reads ownership inside
the same service call that acts, and the DB is the arbiter (unique/constraint
checks, `RowsAffected`). The iam refresh-token reuse detection revokes the family
under the DB's guarantees. No shared mutable state across goroutines beyond the
pool (thread-safe). Nothing actionable found.

## 8 — Infra & config — PARTIALLY FIXED (`4180069`)

- **`securityContext` on the 7 Go service workloads — FIXED (`4180069`)**: added
  `runAsNonRoot` + `runAsUser: 65532` (Dockerfile.svc now pins a numeric UID so
  k8s can verify non-root — a named user can't be verified), `drop: [ALL]`
  capabilities, `allowPrivilegeEscalation: false`, `seccompProfile: RuntimeDefault`.
  `readOnlyRootFilesystem` is named-excluded pending a write-path audit (can't
  confirm no code path writes to the FS without a running instance). Written into
  the manifests, not applied (no cluster) — YAML validated, all 7 asserted.
  **Follow-up:** the third-party infra images (postgres/nats/nginx gateway/migrate)
  still have no `securityContext`; they need image-specific work (unprivileged
  nginx base, postgres UID handling, cap review) rather than a blanket block that
  would risk breaking root-needing images — left for a targeted pass.
- **NATS runs with no authentication** (`00-infra.yaml`): any in-cluster pod can
  read/write all JetStream subjects incl. `core.event.message.>`. Fix: NATS creds
  + per-subject authorization — ops change.
- DB/NATS are `ClusterIP` (not externally exposed); the only external listener is
  the intended gateway NodePort. The k3d dev registry binds `0.0.0.0` (dev only).

**Fronts fixed: 1, 4, 5, 6 fully; 3 in part (CSWSH `986e29e` — the WS handshake
half); 8 in part (`4180069` — the 7 Go workloads).** Still deferred with reasons:
the two committed-credential findings under 3 (published JWT signing key, DB
password) are with Daniel — secret rotation + cross-entrypoint wiring that can't
be validated without a running deployment, higher-risk to half-ship than to
document; and under 8 the third-party infra images (postgres/nats/nginx/migrate)
+ NATS authentication, which need image-specific work rather than a blanket block
that would break root-needing images.
