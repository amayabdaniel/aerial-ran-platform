# aerial-ran-platform

Private AI-native telecom platform for 5–10 users — NVIDIA AI Aerial + OpenAirInterface + Open5GS + FlexRIC + a Go control plane on k3d/k3s.

This is the implementation; full research and architecture rationale live at [`../AI_RAN_PRIVATE_TELECOM_RESEARCH.md`](../AI_RAN_PRIVATE_TELECOM_RESEARCH.md).

## Phase 0 quickstart

```sh
# 1. Bring up Postgres + NATS + observability
make up

# 2. Run migrations
make migrate

# 3. Create k3d cluster
make k3d-up

# 4. Deploy Open5GS + UERANSIM
make ran-up

# 5. Verify a simulated UE attaches
make ran-health
```

Then in another terminal, watch traffic flow:

```sh
make logs        # docker compose logs (control plane)
k9s              # k8s pods (RAN plane)
```

## Repo layout

```
aerial-ran-platform/
├── CLAUDE.md                 # operational rules for AI assistants
├── Makefile                  # daily driver
├── go.work                   # Go workspace
├── docker-compose.yml        # control plane dev stack
├── lib-aerial-go/            # shared library
├── svc-aerial-iam/           # Go service: identity / OIDC
├── svc-aerial-subscriber/    # Go service: SIM/SUPI registry, UDR shadow
├── svc-aerial-esim/          # Go service: Airalo/EMnify adapters
├── svc-aerial-provision/     # Go service: subscriptions
├── svc-aerial-ran-control/   # Go service: xApp host (FlexRIC)
├── svc-aerial-billing/       # Go service: CDR + invoices
├── svc-aerial-messaging/     # Go service: NATS-backed messaging
├── infra/
│   ├── dockerfiles/          # multi-stage Go service Dockerfile
│   ├── k3d/cluster.yaml      # k3d config
│   ├── helm/                 # Helm values overlays (Open5GS, UERANSIM, FlexRIC)
│   ├── nginx/                # API gateway config
│   └── argocd/               # GitOps app-of-apps (later)
├── otel/                     # OTel collector + Prometheus config
├── mobile/                   # iOS (Swift) + Android (Kotlin) clients
├── scripts/                  # bootstrap, migrate, seed
└── docs/                     # runbooks, phase plans, spectrum notes
```

## Phase roadmap (high level)

| Phase | Scope | Hardware | Cost |
|---|---|---|---|
| **0** | k3d + Open5GS + UERANSIM + Go skeletons | existing workstation | $0 |
| **1** | srsRAN/OCUDU + USRP + Open5GS + first xApp | Tier A | ~$5K |
| **2** | Airalo Partners API integration + family eSIMs | — | ~$60/mo OPEX |
| **3** | NVIDIA Aerial + DGX Spark + Foxconn RPQN + PTP | Tier B | ~$46K |
| **4** | Productionization: voice, billing, observability, on-call | — | — |

Detail: §7 of the research doc.

## License

TBD (planned: AGPL-3.0 for services, Apache-2.0 for lib-aerial-go).

## Status

🟡 **Phase 0 in progress.** No production users.
