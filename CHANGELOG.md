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
