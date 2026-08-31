# Error-swallow sweep

A recurring habit in this codebase: an error discarded into a zero value, an
empty slice, a default struct, or a `nil` that reads as absence — surfacing as a
successful-looking wrong answer. This is the full swept list across all 8
modules, with a triage verdict for each.

**Triage rule:** a swallow is *actionable* if the plausible-wrong answer drives a
decision — someone bills on it, provisions on it, or makes an operational/security
call from it. Actionable ones get fixed; the rest are named here so they are not
rediscovered.

## Fixed

| # | Location | Swallow | Wrong answer | Verdict | Commit |
|---|----------|---------|--------------|---------|--------|
| 1 | `svc-aerial-billing/internal/billing/billing.go` MyMonth | any DB error → zero rollup | every user reads `$0.00` during an outage | actionable (billing) | a47a615 |
| 2 | `svc-aerial-subscriber/internal/service/sim.go` Create | Open5GS `Upsert` error dropped | 201 with an "active" SIM a UE can't attach with | actionable (provisioning) | 78193be |
| 3 | `svc-aerial-ran-control/internal/ranctl/ranctl.go` Status | `CountDocuments` error → 0 | `subscribers: 0` reads as an empty network | actionable (operational) | 18437aa |
| 4 | `svc-aerial-subscriber/internal/service/sim.go` Suspend & Terminate | `open5gs.Delete` error dropped | 204 "suspended"/"terminated" for a line the UE can still attach with | actionable (security + billing) | d7ad500 |
| 5 | `svc-aerial-iam/internal/service/iam.go` Refresh (reuse detection) | `RevokeFamily` error dropped | returns `ErrTokenReuse` ("family revoked") while the compromised family stays valid | actionable (security) | this commit |

## Reviewed — actionable, not yet fixed (follow-ups)

_None._ The last actionable item — iam Refresh reuse-detection swallowing
`RevokeFamily` — is now fixed (#5): on a revoke failure the service fails closed
and returns `ErrReuseRevokeFailed` (→ 500 `reuse_revoke_failed`) instead of a
clean "reuse handled". The window is narrow (needs a live reuse attack plus a DB
failure on the revoke), but a security control that fails silently is exactly the
kind that should fail loud.

## Reviewed — not actionable (intentional / durable / inert)

- **`svc-aerial-messaging/internal/messaging/messaging.go:86`** — `Send` discards
  the JetStream publish error. The message row is already committed to Postgres
  (source of truth) and surfaces on the next Inbox poll, so live fan-out is a
  best-effort optimization, not a lost message. Not actionable.
- **`svc-aerial-messaging/.../messaging.go:202`** — `_ = msg.Ack()`. JetStream is
  at-least-once; a missed ack means redelivery, not loss. Not actionable.
- **`svc-aerial-esim/internal/service/esim.go:143`** — `_ = s.prov.Cancel(...)` on
  Cancel. The eSIM is marked cancelled locally regardless; the Airalo adapter's
  `Cancel` is a documented no-op and the mock just drops in-memory state. Low
  cost risk; revisit only if a provider with real cancellation billing is wired.
- **`svc-aerial-ran-control/internal/gnb/gnb.go:182-188`** — `NestedString/…`
  `(value, found, err)` triples ignore `found`/`err`. These read optional fields
  off CRs we created; a missing field correctly defaults (e.g. phase → "Pending").
  Not an error-into-wrong-answer; intentional optional extraction.
- **`lib-aerial-go/respond/respond.go:31`** — `_, _ = w.Write(...)`. A failed
  write means the client already disconnected; nothing actionable remains.
- **`svc-aerial-ran-control/internal/ranctl/ranctl.go:77`** — `if err == nil`
  around the boot-time Mongo ping. Deliberate best-effort init (the service must
  start even if Open5GS lags); the count path (#3) now reports the outage.

## Method

Swept with: `grep 'if err == nil'`, err-branch-returns-default, and
`_ =`/`_, _ =` discards across `svc-aerial-*` + `lib-aerial-go`, excluding
harmless shutdown/close/rand/io.Copy. Re-run those greps after adding code that
touches an external dependency (DB, Mongo, NATS, k8s API) — that boundary is
where this habit recurs.
