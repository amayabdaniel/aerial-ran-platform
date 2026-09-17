# Hardening log

Decisions and invariants worth protecting — especially the ones a future change
would silently break. Each entry names the assumption the fix rests on, so that
when the assumption stops holding the entry, not a production incident, is what
flags it. Companion to `ERROR_SWALLOW_SWEEP.md` (which tracks the error-into-
wrong-value class); this file tracks error-*signal* honesty (right status code,
right blame) and the judgements behind non-changes.

## HTTP status honesty — a caller's mistake must not read as a server fault

A handler that answers 5xx on malformed input is wrong twice: it tells the
client the server is broken, and it adds noise to whatever alerting watches 5xx
rates, paging someone for a fault that does not exist.

| # | Location | Change | Commit |
|---|----------|--------|--------|
| 1 | `svc-aerial-messaging` Send | missing `to_user_id`/`body` now returns 400 via `ErrInvalidMessage`, not 500 | d95efe7 |
| 2 | `lib-aerial-go/respond` DBError | Postgres `22P02` (invalid_text_representation) now maps to 400, not 500 | fd20e90 |

Handler-level validation was already uniform before this run — esim, iam,
subscriber and ran-control all map their validation sentinels (`ErrBadInput`,
`ErrInvalidArgument`, `ErrInvalidRequest`) to 400. messaging (#1) was the sole
gap. #2 covers the layer below: ~24 sites cast client-supplied path/body values
via `$n::uuid`, and a non-parseable value there surfaces as `22P02`.

### Invariant behind #2 — READ THIS BEFORE ADDING DB-casting code

The `22P02 → 400` mapping is correct **only because, in this codebase, every
text→type cast in a query is of a client-supplied value** (a path/body param).
The server never constructs a malformed literal, so `22P02` has no legitimate
server-fault reading here.

**This mapping becomes wrong the day a server-constructed value is cast** — e.g.
a background job, migration, or reconciler that builds a value itself and casts
it. Then a genuine server bug (a bad literal we produced) would be reported to a
client as 400 "your request was malformed", hiding a real fault. If you add such
a path, either validate/parse the value before the query, or narrow the mapping
so it does not cover server-originated casts.

### Deliberate non-change — integrity violations (23xxx) stay 500 at the sink

`respond.DBError` intentionally does **not** map `23xxx` (unique/foreign-key/
not-null violations) to a 4xx. A unique violation's right answer is 409 *with
the resource context the handler has and this shared sink does not* — mapping it
here would ship a contextless 409 from a layer that cannot know what conflicted.
`respond_test.go` asserts `pg_unique_violation_stays_500` to stop a well-meaning
"obviously 23505 should be 4xx too" change from landing at the wrong layer. When
a specific endpoint needs 409, it owns that mapping (see the dropped provision
`ErrSubExists` item — it also needs a schema constraint first).
