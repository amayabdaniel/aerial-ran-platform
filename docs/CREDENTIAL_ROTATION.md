# Credential rotation — burned secrets (2026-10-19)

Two live credentials were committed to this repo as literal values and must be
treated as **compromised permanently**: anything that has ever been in git
history is exposed, and no forward change undoes that.

## What was burned

| Credential | Burned value | Was at |
|---|---|---|
| JWT HS256 signing key | `dev-secret-change-in-production-32ch` | `00-infra.yaml` (the `aerial-secrets` Secret), consumed by all 7 services |
| Postgres password | `aerial_dev_pass_change_me` | `00-infra.yaml`, inline in 7 `DATABASE_URL_*` DSNs |

The JWT key is the more serious: a public HS256 signing key lets anyone forge a
token for any `org`/`role`/`sub`, accepted on every authenticated endpoint.

## What this change did (forward-looking)

- Removed the `aerial-secrets` Secret literal from `00-infra.yaml`.
- The Secret is now produced by a kustomize `secretGenerator` from a **gitignored**
  `infra/k8s/platform/secrets.env`, generated with **fresh random** values by
  `scripts/gen-secrets.sh` (`make k8s-secrets`, run automatically by `make k8s-apply`).
- `secrets.env.example` documents the format; the real file never enters git.
- Net effect: new deployments no longer use the burned values, and future
  credentials are not committed.

This does **not** rotate anything already deployed and does **not** remove the
values from git history — see below.

## Mandatory follow-up (owner action — not done by this change)

1. **Rotate both credentials anywhere they were ever deployed.** This is the real
   mitigation; it makes the leaked values worthless regardless of history. For
   the JWT key, rotating also invalidates every token previously signed with it.
2. **Change the Postgres role password** (`aerial_admin`) in any live DB.
3. The burned literals still live in dev-process-mode files (`docker-compose.yml`,
   the Go config `envOr(...)` defaults, `scripts/`). Those drive `make up`/
   `make run-svcs`, not the k8s Secret, but they keep the literal in the tree —
   scrub them in a follow-up so the only copy is in history.

## History rewrite — assessment

**A history rewrite is required to fully purge the values, but it is the second
priority, not the first.** Rationale:

- The literals are in commit history and in several still-tracked files, so
  removing them from `00-infra.yaml` going forward does not erase them. Anyone
  with a clone or the remote history still has them.
- **Rotation (above) is what actually neutralises the leak.** Once rotated, the
  historical values are inert strings. Do that first.
- A rewrite (`git filter-repo` / BFG) rewrites every commit SHA and forces every
  clone to re-sync — disruptive, and it only matters if the repo was pushed to a
  shared/public remote. Whether to do it is **Daniel's call**; this change does
  not rewrite history.

## Residual, out of scope here

- `sslmode=disable` throughout the DSNs → Postgres traffic (and the password on
  the wire) is plaintext on the pod network. Needs Postgres TLS + `sslmode=require`,
  which needs cert material — a separate hardening task.
