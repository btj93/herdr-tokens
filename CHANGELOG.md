# Changelog

## 0.1.0 — 2026-08-21

First public release.

The `0.1.0` version reflects early public exposure and an API surface that has
not yet been used by anyone outside its author. It does **not** indicate
missing function: every acceptance criterion in the design document passes.

Verified against **Herdr 0.8.2, protocol 20** on macOS.

### Added

- Publishes one status token per workspace, derived from aggregate agent
  status: `st_working`, `st_blocked`, `st_done`, `st_idle`, `st_unknown`,
  `st_none`. Exactly one is set at a time; the rest are cleared.
- `att_blocked` — count of blocked agents, absent when none are blocked.
- `n_agents` — total agent count, absent when the workspace has none.
- TTL heartbeat: tokens expire if the daemon stops, so a dead producer is
  visible rather than leaving a stale colour.
- `start`, `stop`, `daemon`, `validate-config`, `preview`, `version`.

### Notes

- `agent_status` of `null` maps to `st_none`, distinct from `st_unknown`.
  `null` means the workspace has no agent; `unknown` means an agent is present
  but could not be classified.
- The plugin subscribes to no events. Polling doubles as the TTL heartbeat,
  which avoids a metadata feedback loop.

### Fixture privacy guard — replaced the shape-based denylist with a closed positive schema

This was originally recorded here as a *known limitation*: `scripts/sanitize.py`,
the fixture tests, and `.githooks/pre-commit` detected private data by
scanning for *known-bad shapes* — a `/Users/` path that is not the
placeholder, a tilde path, a non-generic `terminal_title`, a UUID, a
`term_`-prefixed identifier. That is a denylist, and a denylist has a specific
blind spot worth stating plainly:

**A scan for known-bad shapes cannot distinguish a leak from its own fix.**

This was demonstrated, not theorised. Two independently written audit tools
each cleared their own repository and each raised false positives against the
*other's* placeholders — one pattern matched any `term_` containing a hex
letter and so flagged the replacement value `term_00000000abcd`; the other
excluded only `term_0+` and so flagged the placeholder `term_000000000001`.
Both tools were correct about the data and wrong about the other's convention.

A further consequence, also observed: fixing a fixture by regenerating it
corrects `HEAD` and leaves every historical blob untouched, while every
subsequent working-tree check reports success. Scan every object in history,
not the files you just edited.

**This is now implemented, before publication, rather than deferred to
v0.1.1.** `internal/herdrapi/schema.go` inverts the guard: it asserts that a
decoded fixture conforms to a **positive schema** of permitted fields and
value shapes (a literal, an enum, or a regex per field — e.g. `terminal_id`
must match `^term_\d{12}$`, `agent_session.value` must match
`^00000000-0000-4000-8000-\d{12}$`), rather than scanning for a growing list
of forbidden ones. Critically, the schema is **closed**: a field present in a
fixture but not named in the schema for its enclosing object fails, naming
the field, instead of passing through unexamined — a future Herdr version
that adds a field anywhere in the tree fails the build the first time a
fixture containing it is captured, until a human classifies it. Both
`term_00000000abcd` and `term_000000000001` — the two placeholder conventions
that fooled each other's denylist above — now fail the same check, for the
same reason: neither is `^term_\d{12}$`. Placeholders are deliberately
conformant now, rather than merely distinguishable-by-convention from a live
value.

`scripts/sanitize.py` was updated to emit schema-conformant placeholders
(`term_<12 digits>`, `00000000-0000-4000-8000-<12 digits>`), `testdata/*.json`
were re-sanitized and hand-verified against the schema, and
`.githooks/pre-commit` now builds and runs the schema checker
(`cmd/schema-check`) against every staged `testdata/` blob. The five denylist
checks the schema strictly subsumes (paths, tilde paths, titles, UUIDs,
`term_` identifiers) were deleted rather than kept alongside it; one denylist
check survives deliberately — a raw scan for the committing user's account
name anywhere in the blob — because a few schema fields (protocol-internal
descriptors, not user data) are intentionally type-checked only, and a leaked
account name could otherwise hide in one of those. See
`internal/herdrapi/schema.go`'s header comment and the README's "Why a
schema, not a denylist" section for the full reasoning.
