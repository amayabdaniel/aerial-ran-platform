# AERIAL-RAN-PLATFORM

Private AI-native telecom platform. Small footprint (5–10 users). Built on **NVIDIA AI Aerial** (L1 GPU PHY) + **OpenAirInterface** (L2/L3) + **Open5GS** (5GC) + **FlexRIC** (Near-RT RIC) + a Go control plane on **k3d/k3s**.

Full research/design at `../AI_RAN_PRIVATE_TELECOM_RESEARCH.md` (~14.5K words). Read that for context. The tl;dr is in §10 of that doc.

## Architecture (control plane)

Go microservices on k3d (dev) / k3s (lab), Postgres schema-per-service, NATS Jetstream for events, OpenTelemetry everywhere. Mobile clients in Swift (iOS) + Kotlin (Android).

| Service | Port | Schema | Path Prefix | Responsibility |
|---------|------|--------|-------------|---------------|
| svc-aerial-iam            | 8081 | iam            | `/iam/v1/`        | OIDC, users, devices, JWT (Ory Kratos backend) |
| svc-aerial-subscriber     | 8082 | subscriber     | `/subscriber/v1/` | SUPI/IMSI/Ki registry; UDR-shadow for Open5GS; sysmoISIM provisioning |
| svc-aerial-esim           | 8083 | esim           | `/esim/v1/`       | Airalo/EMnify/Soracom adapter; LPA QR issuance; lifecycle webhooks |
| svc-aerial-provision      | 8084 | provision      | `/provision/v1/`  | Customer subscriptions, plan changes, top-ups |
| svc-aerial-ran-control    | 8085 | ranctl         | `/ran/v1/`        | xApp host (FlexRIC E2/KPM/RC); ingests RAN KPIs; closed-loop control |
| svc-aerial-billing        | 8086 | billing        | `/billing/v1/`    | CDR ingest → usage rollups → invoices |
| svc-aerial-messaging      | 8087 | messaging      | `/msg/v1/`        | E2E messaging over NATS Jetstream; presence |

Shared library: `lib-aerial-go` (auth, cors, etag, httplog, metrics, ratelimit, recover, respond, secure, timeout, tracing, health).

## RAN plane (not the control plane)

| Component | Software | Compute | Notes |
|---|---|---|---|
| 5GC          | Open5GS v2.7.6 (Helm `towards5gs-helm`) | k3d pods | dev/Phase 0 |
| 5GC (prod)   | OAI CN5G (Helm `oai-cn5g-fed`)         | k3s pods | when paired with Aerial |
| gNB sim      | UERANSIM v3.2.8                         | k3d pods | Phase 0 |
| gNB real     | OAI gNB CU/DU (weekly tag pinned)       | bare metal | Phase 3 |
| L1           | NVIDIA Aerial CUDA-Accelerated RAN 26.1 | GPU bare metal (DGX Spark / GH200) | Phase 3 |
| O-RU         | Foxconn RPQN-7801 (n78 4T4R)            | hardware | Phase 3 |
| Near-RT RIC  | FlexRIC                                  | k3d pods | Phase 1 |
| Non-RT RIC   | O-RAN SC nonrtric                       | k3s pods (optional) | Phase 4 |

Open5GS, UERANSIM, FlexRIC are referenced via Helm — they do NOT live in this repo. Their values overlays live in `infra/helm/`.

## Code Structure (per Go service)

```
svc-aerial-{name}/
  cmd/server/main.go             # Entry, wiring, middleware chain
  internal/
    config/config.go             # Env loading (envconfig)
    handler/                     # HTTP handlers (thin — parse, validate, delegate)
    service/                     # Business logic
    repository/                  # PostgreSQL (pgxpool)
    model/                       # Domain types, validation, sentinel errors
    cache/                       # In-memory TTL where useful
  migrations/                    # SQL (numbered up/down)
  Dockerfile
  Makefile
  README.md
```

## Middleware Chain (all services)

```
recover → metrics → tracing → secure → cors → httplog → ratelimit → timeout → body_limit → [auth] → handler
```

Timeout 15 s; body limit 4 MB; rate-limit 100 req/sec per IP default.

## Key Patterns

- **`respond.JSON/Error`**: pooled JSON responses; maps `context.Deadline` → 504, `context.Canceled` → 499.
- **Sentinel errors**: `model.ErrXxxNotFound`; check with `errors.Is()`.
- **Transaction support**: `repository.DBTX` + `TxBeginner` interface (events/payroll pattern from backend-log).
- **Background health**: `atomic.Bool` updated every 5s, not per-request DB ping.
- **HMAC request signing**: optional `X-Signature` + `X-Timestamp` via `lib-aerial-go/hmacauth`.
- **Device-bound refresh tokens**: refresh tokens carry `device_id`, families with reuse detection.
- **NATS Jetstream contracts**: subjects in `core.event.*`; one consumer per service; retention by interest.
- **OTel auto-instrumentation**: all HTTP + DB calls; SDK pre-wired in `lib-aerial-go/tracing`.

## NATS Subjects (control plane)

```
core.event.subscriber.created/updated/deleted
core.event.esim.provisioned/topup/usage
core.event.session.started/ended      (from Open5GS SBI webhook adapter)
core.event.voip.call.started/ended    (CDR feed)
core.event.message.sent/delivered
core.event.ran.kpi                    (from FlexRIC xApp via KPM v3)
core.event.ran.policy.applied         (RC v1 actions taken)
core.event.ai.inference.requested/completed
core.event.alert.fired
```

## Spectrum / regulatory posture (default)

- Default test PLMN: **MCC=999 MNC=70** (per 3GPP TS 23.003, reserved for test networks)
- Phase 0: pure simulation in k3d — no RF
- Phase 1+: 6 GHz NR-U LPI (unlicensed) preferred; CBRS GAA n48 with SAS subscription as fallback (US)
- Colombia: only libre-uso bands (Res. ANE 711/2016) until MinTIC permiso pruebas técnicas issued
- See `docs/SPECTRUM.md` and §9 of research doc

## Commands

```sh
# Bring up dev stack (postgres, nats, otel, prom, grafana, loki, jaeger)
make up

# Run migrations
make migrate

# Bring up k3d cluster + Open5GS + UERANSIM
make k3d-up
make ran-up

# Check Open5GS health
make ran-health

# Run all unit tests
make test-unit

# Run all integration tests (testcontainers)
make test-integration

# Daily: stack up + tests + security scan
make daily

# Tear everything down
make down k3d-down
```

## Conventions

1. **NEVER commit secrets.** `.env` is gitignored; use `.env.example` to document keys.
2. **Module-gated features**: each new endpoint set requires a `module` flag on the org; `auth.RequireModule("module_name")` middleware enforces.
3. **All multi-table writes use transactions** (`TxBeginner` pattern).
4. **Sentinel errors only** — never string-compare errors.
5. **Handlers are thin**: parse → validate → service → respond.
6. **OTel everywhere**: every external call gets a span; gRPC + HTTP + DB auto-instrumented.
7. **Prom metrics on `/metrics`**: RED + DB pool gauges. RAN KPIs to ClickHouse via OTel exporter.
8. **Spanish UI text** when user-facing (CO operator UX).
9. **Currency**: COP integer cents; `FormatCOP()` helper for display.
10. **DON'T copy production patterns blindly** from `../backend-log`; this is a smaller team with different SLOs. Keep services lean.

## Observability

- Prometheus `/metrics` (RED + DB pool)
- OTel traces → Tempo (or Jaeger in dev)
- Loki for logs (Promtail scrapes Docker)
- Grafana provisioned dashboards
- Slow query log >200 ms (Postgres `log_min_duration_statement`)
- DCGM exporter for GPU (Phase 3)
- Aerial Sample Apps exporter for L1 KPIs (Phase 3)
- KPM v3 → FlexRIC xApp → NATS → ClickHouse (Phase 2+)

## Mobile clients

- iOS: `mobile/ios/` (Swift, SwiftUI, URLSession + Apollo for GraphQL, WebRTC.framework, CallKit/PushKit)
- Android: `mobile/android/` (Kotlin, Compose, Ktor, WebRTC, ConnectionService)
- Both consume the same gRPC-Web gateway via `svc-aerial-iam`.

## Reference docs

- `../AI_RAN_PRIVATE_TELECOM_RESEARCH.md` — full research / design / cost / spectrum
- `docs/DEV_RUNBOOK.md` — local setup
- `docs/PHASE_0.md` — Phase 0 checklist
- `docs/PHASES.md` — full phased roadmap
- `docs/SPECTRUM.md` — regulatory posture

## Hard rules (read before edits)

- NEVER push `.github/workflows/*.yml` without explicit user confirmation (limited Actions minutes).
- NEVER include `Co-Authored-By` in commits.
- Single-line commit messages; no trailers.
- Don't commit binary RF captures (`.pcap`, `.iq`, `.sigmf-*`) — gitignored.
- Don't commit Ki/OPc keys — use Vault or `.env` (gitignored).
- Don't ship Aerial proprietary binaries in this repo — link to NGC.
