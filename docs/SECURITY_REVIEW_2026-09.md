# Security review — 2026-09-19

Attack-surface enumeration across eight vectors for the aerial-ran-platform
control plane (7 Go services + `lib-aerial-go`, Postgres/pgx, NATS, Open5GS
Mongo, k3d/k8s). Each vector carries an exposure verdict; fixed items cite the
commit, deliberately-unfixed items say why. Companion to `HARDENING_LOG.md`.

## Summary

| # | Vector | Verdict | Action |
|---|--------|---------|--------|
| 1 | AuthN/AuthZ | **EXPOSED** | **FIXED** `880b94c` — cross-tenant IDOR closed |
| 2 | Injection | not exposed | none needed (parameterized/constant) |
| 3 | Transport & secrets | **EXPOSED** | documented; ops+cross-entrypoint change (below) |
| 4 | Input handling & DoS | **EXPOSED** | **FIXED** `09c657e` — 4 MiB body limit |
| 5 | Supply chain | **EXPOSED** | **FIXED** `9726451` — x/text→v0.39.0; digest pinning documented |
| 6 | Data exposure | **EXPOSED** | **FIXED** `67aab62` — DBError no longer leaks driver text |
| 7 | Concurrency & state | not exposed (see below) | none needed |
| 8 | Infra & config | **EXPOSED** | documented; requires manifest apply (below) |

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

- **Known public JWT HS256 secret** (`dev-secret-change-in-production-32ch`) is
  the committed value in `infra/k8s/platform/00-infra.yaml` **and** the hardcoded
  Go default; validation only checks `len >= 16`. A public signing key means
  anyone can forge tokens for any `org`/`role`/`sub` on every authenticated
  endpoint. **This is the most severe finding.** The correct fix is not a one-line
  patch: rotate the secret out of git into a real secret store, AND add a
  fail-closed guard (`jwt.CheckSecret` rejecting empty/short/known-placeholder
  unless `ALLOW_INSECURE_JWT_SECRET=true`) wired into **every** entrypoint
  (`runner.Run` + the iam/subscriber/esim custom mains), AND set the dev opt-in in
  the committed dev manifests so `make up`/k8s still boot. That spans code + ops +
  manifests and cannot be validated without a running deployment; a half-wired
  guard that breaks dev boot is worse than the documented finding, so it is
  flagged here rather than partially shipped. **Recommended next action, high
  priority.**
- **Postgres password committed inline** in `00-infra.yaml` with `sslmode=disable`
  (plaintext DB traffic). Fix is ops: templated Secret + TLS. Cannot apply
  manifests in this pass.
- **WebSocket CSWSH**: `svc-aerial-messaging` sets `InsecureSkipVerify: true` on
  `websocket.Accept`, disabling Origin checking (cross-site WebSocket hijack if
  the service is reached directly, bypassing the gateway). Fix: drop the flag
  (the library then enforces same-origin) and set `OriginPatterns` from
  `ALLOWED_ORIGINS`. Contained, but the WS-upgrade path is not cleanly unit-
  testable via `httptest` (no hijack support), so deferred to a targeted change.

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

## 8 — Infra & config — EXPOSED (documented, requires manifest apply)

- **No `securityContext` on any workload** in `infra/k8s/` (no `runAsNonRoot`,
  `readOnlyRootFilesystem`, dropped capabilities, seccomp); gateway/migrate images
  run as root. Fix is manifest hardening — cannot apply k8s here.
- **NATS runs with no authentication** (`00-infra.yaml`): any in-cluster pod can
  read/write all JetStream subjects incl. `core.event.message.>`. Fix: NATS creds
  + per-subject authorization — ops change.
- DB/NATS are `ClusterIP` (not externally exposed); the only external listener is
  the intended gateway NodePort. The k3d dev registry binds `0.0.0.0` (dev only).

**Fronts fixed: 1, 4, 5, 6 (four distinct vectors here; vector 8 fixed in the
wavekube half of this pass). Deliberately deferred with reasons: 3 and 8** —
both need secret rotation / manifest application / cross-entrypoint wiring that
cannot be validated without a running deployment, and are higher-risk to
half-ship than to document with a concrete recommended fix.
