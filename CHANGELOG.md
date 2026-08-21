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

### Known limitation — fixture privacy guards are shape-based

`scripts/sanitize.py`, the fixture tests, and `.githooks/pre-commit` detect
private data by scanning for *known-bad shapes*: a `/Users/` path that is not
the placeholder, a tilde path, a non-generic `terminal_title`, a UUID, a
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

The v0.1.1 direction is to invert this — assert that fixtures conform to a
**positive schema** of permitted values, rather than scanning for a growing
list of forbidden ones. An allowlist would have returned clean for both tools
on the first pass. Until then, treat a novel identifier shape as likely rather
than hypothetical: three distinct classes of private data were found here in
sequence, each invisible to the rule that caught the previous one.
