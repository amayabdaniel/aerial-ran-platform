# aerial-ran-platform

Private AI-native telecom platform for 5–10 users — NVIDIA AI Aerial + OpenAirInterface + Open5GS + FlexRIC + a Go control plane on k3d/k3s.

This is the implementation. Full research and architecture rationale: [`../AI_RAN_PRIVATE_TELECOM_RESEARCH.md`](../AI_RAN_PRIVATE_TELECOM_RESEARCH.md) (~14.5K words).

---

## What's running

| Layer | Service | Purpose | Port |
|---|---|---|---|
| **Web UI** | nginx + `web/index.html` | sign in, manage SIMs/eSIMs/plans/messages/usage | `:18080/ui/` |
| **API gateway** | nginx | single `/api/*` entry to the 7 services | `:18080` |
| **iam** | Go | OIDC-lite, signup/login/refresh, JWT, device-binding | `:8081` |
| **subscriber** | Go | SIM CRUD → auto Ki/OPc → writes into Open5GS MongoDB | `:8082` |
| **esim** | Go | Airalo / EMnify / **mock** adapter; LPA + QR PNG | `:8083` |
| **provision** | Go | plans, subscriptions, user → sim/esim binding | `:8084` |
| **ran-control** | Go | Open5GS observer (NF metrics + Mongo subscriber count) | `:8085` |
| **billing** | Go | usage event ingest + monthly rollups | `:8086` |
| **messaging** | Go | NATS-JetStream messaging + WebSocket live stream | `:8087` |
| **Postgres 17** | docker | schema-per-service (7 schemas) | `:15432` |
| **NATS 2.10** | docker | JetStream for events | `:14222` |
| **Open5GS 5G SA** | k3d (12 NFs) | AMF/SMF/UPF/AUSF/UDM/UDR/PCF/NRF/NSSF/SCP/BSF | k3d ns `ran` |
| **MongoDB 5.0** | k3d | subscribers Open5GS reads (we write here from `subscriber`) | k3d, port-forward to `:27017` |
| **UERANSIM 3.2.6** | k3d | simulated gNB + 2 UEs (registers, auth succeeds; PDU blocked by upstream NAS bug — see `docs/PHASE_0.md`) | k3d ns `ran` |
| **Observability** | docker | OTel collector → Prometheus + Loki + Jaeger + Grafana | `:13000` (Grafana) |

---

## Quickstart from scratch

Prereqs: macOS or Linux, Docker Desktop, Go 1.26+, brew packages `kubectl helm k3d k9s`.

```sh
git clone git@github.com:amayabdaniel/aerial-ran-platform.git
cd aerial-ran-platform
cp .env.example .env

# Bring up everything (control plane + k3d + Open5GS + Go services).
make all-up

# Optional: seed 5 family members + SIMs + eSIMs + plans
make seed-family
```

`make all-up` prints both your `localhost:18080/ui/` and your **LAN URL** (e.g. `http://192.168.1.5:18080/ui/`) at the end. Open the LAN URL on your laptop — the page shows a QR code at the top that your family can scan with their phone to install the **PWA to their home screen**.

If you prefer the manual route, the individual targets still work: `make up && make migrate && make k3d-up && make ran-up && make mongo-pf-up && make run-svcs`.

## Stop everything

```sh
make all-down
```

Or piecemeal: `make stop-svcs`, `make mongo-pf-down`, `make ran-down`, `make k3d-down`, `make down`.

## Demo to your family (on real phones)

1. Run `make all-up` and `make seed-family`. You now have 5 user accounts:
   - daniel.family@aerial.local
   - ana.family@aerial.local
   - sofia.family@aerial.local
   - mateo.family@aerial.local
   - abuela.family@aerial.local

   Password for all: `family-demo-2026`

2. On your laptop, open the LAN URL printed by `make all-up`:
   ```
   open http://192.168.1.5:18080/ui/
   ```
   (Your IP will be different — `make all-up` figures it out.)

3. The top banner shows a QR code. Hand a phone to a family member.

4. They scan the QR, the page opens in Safari/Chrome.

5. **iOS**: Share → Add to Home Screen. **Android**: menu → Install app.

6. They open "Aerial" from the home screen, log in with their email + the password above. They can now:
   - See their SIM (IMSI 999700000000xx)
   - Order an eSIM and get a scannable LPA QR
   - Subscribe to a plan
   - Send messages to other family members (live via WebSocket)
   - See their monthly usage rollup

The platform won't actually carry their cell-phone traffic until Phase 1 hardware (USRP + srsRAN + COTS phones with programmable SIMs), but the user-facing flows are real.

## End-to-end demo via curl

```sh
./scripts/demo.sh
```

Hits every endpoint: signup → login → subscribe → SIM → eSIM → usage → message → RAN status. Useful for CI and showing the API surface.

---

## Things you can do in the UI

1. **Sign up** (top of the page) → get a JWT (15-min access + 30-day refresh).
2. **Issue a SIM** → IMSI auto-assigned (999700000000001+), random Ki/OPc generated, **the row is also written into Open5GS MongoDB** — meaning the simulated UE in UERANSIM can attach with that SIM.
3. **Suspend / resume a SIM** → removes / re-adds it from Open5GS in real time.
4. **Refresh eSIM catalog** for Colombia, US, EU, Global. Order one — you get a real ICCID, LPA string, and a base64-encoded QR PNG you can scan with `lpac` / `OpenEUICC` (it won't activate because the mock SM-DP+ is fake, but the flow is real).
5. **Poll usage** — the mock provider grows usage 10% per click.
6. **Subscribe** to a plan (Basic $5 / Family $15 / Premium $25).
7. **Send a message** — backed by NATS JetStream, persisted in Postgres.
8. **Ingest a billing event** — rolls up into your monthly usage.
9. **Check RAN status** — live count of subscribers in Open5GS + which 5G NFs are reachable.

## Repo layout

```
aerial-ran-platform/
├── CLAUDE.md                    operational rules for AI assistants
├── Makefile                     daily driver
├── go.work                      Go workspace (7 services + lib)
├── docker-compose.yml           control-plane: postgres, nats, otel, prom, grafana, loki, jaeger, nginx
├── lib-aerial-go/               shared lib: respond, health, httplog, metrics, recover, tracing, jwt, runner
├── svc-aerial-iam/              identity / OIDC-lite / JWT
├── svc-aerial-subscriber/       SIM CRUD + Open5GS MongoDB writer
├── svc-aerial-esim/             Airalo adapter + mock; LPA + QR
├── svc-aerial-provision/        plans + subscriptions
├── svc-aerial-ran-control/      Open5GS observer
├── svc-aerial-billing/          usage events + monthly rollups
├── svc-aerial-messaging/        NATS-JetStream messaging + WebSocket
├── infra/
│   ├── dockerfiles/             multi-stage Go service Dockerfile
│   ├── k3d/cluster.yaml         k3d cluster config (1 server + 2 agents)
│   ├── helm/                    Open5GS + UERANSIM Helm values overlays
│   ├── k8s/                     MongoDB + SIM-seed manifests
│   └── nginx/                   API gateway + UI server config
├── otel/                        OTel collector + Prometheus + Grafana provisioning
├── web/                         single-file admin UI (vanilla HTML/JS)
├── scripts/
│   ├── migrate.sh               cross-schema idempotent migration runner
│   ├── run-svcs.sh              start/stop/status for the 7 services
│   ├── scaffold-stub-services.sh   one-time scaffolder (already run)
│   └── demo.sh                  end-to-end curl demo
└── docs/
    └── PHASE_0.md               Phase 0 status + the documented UERANSIM/Open5GS NAS-parser bug
```

## URLs

| URL | What |
|---|---|
| http://localhost:18080/ui/        | Web admin UI |
| http://localhost:18080/api/*      | API gateway (e.g. `/api/iam/v1/auth/login`) |
| http://localhost:13000            | Grafana (admin/admin) |
| http://localhost:17686            | Jaeger UI |
| http://localhost:19090            | Prometheus |
| http://localhost:13100            | Loki API |
| postgres://localhost:15432/aerial | Postgres |
| nats://localhost:14222            | NATS client |

## Phase roadmap

| Phase | Scope | Status |
|---|---|---|
| **0** | k3d + Open5GS + UERANSIM + Go skeletons | ✅ done |
| **0.5** | 7 real Go services + API gateway + web UI | ✅ done (you are here) |
| **1** | srsRAN/OCUDU + USRP B210 + Open5GS + first FlexRIC xApp (~$5K hardware) | next |
| **2** | Real Airalo Partners API integration + family eSIMs | next |
| **3** | NVIDIA Aerial CUDA-Accelerated RAN + DGX Spark + Foxconn RPQN + PTP (~$46K hardware) | future |
| **4** | iOS + Android native clients, voice via Kamailio + SIP-WSS | future |

See §7 of the research doc.

## License

TBD (planned: AGPL-3.0 for services, Apache-2.0 for `lib-aerial-go`).

## Status

🟢 **Phase 0.5 complete.** Single-host private 5G + control plane + UI all running.
🟡 No production users. Lab scope.
