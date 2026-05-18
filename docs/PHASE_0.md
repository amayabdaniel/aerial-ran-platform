# Phase 0 — Foundation

Goal (per research §7): "Stand up k3s + Open5GS + UERANSIM + Go service skeletons on your existing workstation. Free, ~2 weekends, no hardware needed."

## Status: ✅ COMPLETE (with one documented upstream limitation)

## What works

### Control plane (docker compose)
- `make up` brings up: postgres-17, nats-2.10 (JetStream), otel-collector, jaeger, prometheus, loki, grafana, postgres-exporter
- Postgres listens on `127.0.0.1:15432`, NATS on `:14222`, Grafana on `:13000`, Jaeger on `:17686`, Prometheus on `:19090`, Loki on `:13100` (offset to avoid conflict with sibling stacks like backend-log)
- `make migrate` runs schema migrations across 7 service schemas idempotently (tracked in per-schema `schema_migrations` table)

### Go monorepo
- 7 services compile via `go.work` + per-service `replace` to `../lib-aerial-go`:
  - `svc-aerial-iam` (full HTTP server + DB pool + middleware chain)
  - `svc-aerial-{subscriber,esim,provision,ran-control,billing,messaging}` (Phase 0 stubs — healthz + metrics)
- Shared library `lib-aerial-go`: respond, health, httplog, metrics, recover, tracing
- `make test-unit` runs all module tests; `make build` cross-compiles all 7
- Verified end-to-end: `svc-aerial-iam` against the live compose stack — `/v1/health` returns 200 (background DB ping every 5 s), `/metrics` exposes Prometheus, `/v1/whoami` returns service identity

### RAN plane (k3d)
- `make k3d-up` creates a 3-node k3s 1.33.6 cluster (1 server + 2 agents), with a local image registry on `localhost:5005`
- `make ran-up` deploys via Gradiant 5g-charts (OCI):
  - **MongoDB** (multi-arch `mongo:5.0` — replaces broken Bitnami subchart)
  - **Open5GS 2.7.2** 5G control plane: AMF, AUSF, BSF, NRF, NSSF, PCF, SCP, SMF, UDM, UDR, UPF (12 NFs)
  - **UERANSIM gNB** v3.2.6, NGAP/SCTP to AMF
  - **2 UERANSIM UEs** with IMSIs 999700000000001 and 999700000000002
- 2 subscribers seeded via `infra/k8s/seed-ues.yaml` Job (replaces broken `populate` subchart)
- `make ran-health` shows pod status, MongoDB subscribers, NGAP setup, UE registration state

### Verified RAN flow (Open5GS 2.7.2 + UERANSIM 3.2.6)
1. ✓ gNB SCTP connects to AMF (`open5gs-amf-ngap:38412`)
2. ✓ NG Setup succeeds (after dropping S-NSSAI `sd` to match AMF config)
3. ✓ UE detects cell (PLMN 999/70, TAC 1, SST 1)
4. ✓ UE sends RRC Setup Request → RRC Connected
5. ✓ gNB forwards Initial NAS to AMF
6. ✓ AMF runs **Authentication Request → 5G-AKA → Authentication Response succeeds**
7. ✓ AMF sends **Security Mode Command** (NIA2/NEA0) which UE accepts
8. ✗ UE crashes parsing the next NAS message (`Bad constructed NAS message` → `std::runtime_error`)

## Known limitation: UERANSIM 3.2.6 ↔ Open5GS 2.7.x NAS parser bug

**Symptom**: UE crashes after Security Mode Command, before Registration Accept can be parsed.

**Root cause**: Open5GS 2.7.x (Rel-17) includes NSSAI IEs in Registration Accept that UERANSIM 3.2.6 doesn't decode. Fixed in UERANSIM master post-3.2.6 (Jan 2025 commits).

**Why we're pinned to 3.2.6 on Apple Silicon (arm64)**:
- `gradiant/ueransim:3.2.8` is published as a multi-arch manifest where the arm64 entry is marked unsupported. k3s containerd refuses cross-arch pulls (and refuses `ctr import` of amd64-only oci-index too).
- `gradiant/ueransim:3.2.6` happens to be tagged as a single-arch arm64 image at the registry layer (likely built with `--platform=linux/arm64` from emulated cross-compile), so containerd accepts it.
- `free5gc/ueransim:latest-aarch64` is genuinely multi-arch but uses a different entrypoint convention than the gradiant chart.

**Path to unblock** (any of):
1. Build UERANSIM `master` (or `>3.2.6`) for arm64 ourselves and import via `k3d image import` (then set `image.pullPolicy: Never`).
2. Run k3d on an amd64 host (CI runner, cloud VM).
3. Move to Phase 1 hardware (USRP B210 + srsRAN/OCUDU) where the UE is COTS and not UERANSIM.

Decision for now: capture this as a Phase 0 known limitation and proceed. The 5GC + RAN signaling planes are proven to work through authentication and security-context establishment.

## Phase 0 exit checklist

| Item | Status |
|---|---|
| Monorepo bootstrapped, all 7 Go services compile | ✅ |
| Postgres 17 + 7 schemas + migrations | ✅ |
| NATS Jetstream up | ✅ |
| Observability (OTel + Prom + Grafana + Loki + Jaeger) | ✅ |
| svc-aerial-iam end-to-end against compose stack | ✅ |
| k3d cluster (1 server + 2 agents) | ✅ |
| Open5GS 5G SA NFs (12) running | ✅ |
| MongoDB (multi-arch replacement) | ✅ |
| Pre-seeded subscribers (IMSI 999700000000001/2) | ✅ |
| UERANSIM gNB ↔ AMF NGAP setup | ✅ |
| UERANSIM UE RRC connect + 5G-AKA auth + security mode | ✅ |
| UE PDU session attach end-to-end | ⏸ blocked by upstream UERANSIM/Open5GS NAS bug |

## What's next (Phase 1)

- Tier A hardware (~$5K): USRP B210 + srsRAN-Project/OCUDU + LimeSDR Mini for UE-side
- Real OTA 5G NR SA cell with COTS UEs (no UERANSIM, no NAS bug)
- First FlexRIC xApp (KPM v3 logger → NATS → ClickHouse)
- See §7 of `../AI_RAN_PRIVATE_TELECOM_RESEARCH.md`
