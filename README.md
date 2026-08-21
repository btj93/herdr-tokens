# herdr-tokens

**Version 0.1.0** — a [Herdr](https://herdr.dev) plugin (requires Herdr
`>= 0.8.0`; verified against Herdr `0.8.2`, protocol `20`) that publishes
workspace metadata tokens derived from agent status.

## What it does

`herdr-tokens` polls the running Herdr session and, for every workspace,
writes a small set of metadata tokens describing that workspace's agent
state. It decides which token **name** is written — for example, that a
workspace with a working agent gets `st_working` rather than `st_idle`. It
does **not** decide what that token looks like. Colour, weight, and every
other visual choice belong to the token's *consumer*: your own Herdr
configuration, or another plugin such as a tabline renderer.

There are no colour options in this plugin, in its configuration file, or in
its manifest. There never will be — that would duplicate a decision that
already belongs to the consumer, and would fight it over which value wins.

## Install

```
cd herdr-tokens
herdr plugin link
```

Herdr builds the plugin using the `[[build]]` command in
`herdr-plugin.toml` (`go build -o bin/herdr-tokens ./cmd/herdr-tokens`) and
starts the daemon automatically, both on Herdr startup and whenever a new
pane is created (`[[startup]]` / `[[events]] on = "pane.created"`), so a
freshly opened session always has a live producer without you needing to run
anything by hand. `start`, `stop`, `validate-config`, and `preview` are also
exposed as manifest actions if you want to drive them from Herdr's action
picker instead of the CLI directly.

### Consumer setup

`herdr-tokens` only writes tokens; something must be configured to *read*
them for you to see anything. The most common consumer is Herdr's own
sidebar. Add all **six** `st_*` variants to your `[ui.sidebar.spaces]` row,
in your own `~/.config/herdr/config.toml`:

```toml
[ui.sidebar.spaces]
rows = [[
  "state_icon",
  "$st_working", "$st_blocked", "$st_done", "$st_idle", "$st_unknown", "$st_none",
]]
```

Two things about this row are easy to get wrong:

- **You must list all six variants, including `$st_none`.** Exactly one
  `st_*` token is set on any given workspace at any time; the rest are
  cleared. If you leave `$st_none` out of the row because it "does nothing" —
  it's the variant that fires on every ordinary workspace with no agent
  running, so it is also the one variant nobody happens to test by hand —
  then the row has nothing to render for every agent-free workspace, and
  **the workspace's name disappears from the sidebar entirely**, not just its
  colour. The name is carried *inside* the token (see Configuration below),
  not displayed alongside it, so there is no fallback text left once the
  token is absent.
- **Do not also include a plain `workspace` token in the same row.** The
  default sidebar row ships as `rows = [["state_icon", "workspace"]]`. If you
  keep `"workspace"` alongside the six `st_*` entries, the workspace name
  renders twice: once from the plain built-in token, once from whichever
  `st_*` token happens to be set. Replace `"workspace"` with the six `st_*`
  entries; don't add to it.

Per-token colour, dimming, boldness, etc. are configured entirely on the
consumer side (Herdr's own UI configuration, or the reading plugin's own
docs) — this plugin has no say in and no knowledge of how any token is
styled.

## Token reference

Exactly one of the six `st_*` tokens is set per workspace at any tick (the
rest are explicitly cleared); `att_blocked` and `n_agents` are independent
counts, and are **absent** — not present with a value of `"0"` — whenever
they don't apply. These eight names, once published, are a **stable public
contract**: they are already consumed by other plugins (e.g. a tabline
renderer), and this plugin will not rename or repurpose any of them.

| Token         | Set when                                                              | Value carried |
|---------------|------------------------------------------------------------------------|----------------|
| `st_working`  | the workspace's agent status is `working`                              | workspace label (or the status word, if configured — see below) |
| `st_blocked`  | the workspace's agent status is `blocked`                               | workspace label (or status word) |
| `st_done`     | the workspace's agent status is `done`                                  | workspace label (or status word) |
| `st_idle`     | the workspace's agent status is `idle`                                  | workspace label (or status word) |
| `st_unknown`  | an agent is present but Herdr could not classify its status (`"unknown"`) | workspace label (or status word) |
| `st_none`     | the workspace has **no agent at all** (`agent_status` is `null`)        | workspace label (or status word) |
| `att_blocked` | one or more agents in the workspace are blocked                        | decimal count of blocked agents; **absent** if none are blocked |
| `n_agents`    | the workspace has one or more agents                                   | decimal count of all agents in the workspace; **absent** if there are none |

`st_none` and `st_unknown` are deliberately distinct: `null` means "no agent
here", `"unknown"` means "an agent is here, but Herdr can't tell you its
state." Collapsing the two would paint every ordinary, agent-free workspace
with the same colour as a workspace whose agent is stuck in a state Herdr
doesn't recognize.

## Configuration

`herdr-tokens` reads an optional `config.toml` (schema below is
`config.example.toml`, copy it and edit as needed). A missing file is valid
and yields these defaults:

| Field           | Default | Meaning |
|-----------------|---------|---------|
| `schema_version` | `1`    | Config schema version; unknown versions are rejected. |
| `poll_interval`  | `"3s"` | How often the daemon polls the session snapshot. Also the heartbeat period. |
| `ttl`            | `"90s"` | Token lifetime handed to Herdr on every write. Must be at least `3 × poll_interval`, so a single missed tick can never expire a token. If the daemon stops, tokens expire after this long. |
| `value`          | `"label"` | What the `st_*` tokens carry: `"label"` writes the workspace name (so the *name* renders in the status colour); `"status"` writes the status word instead — safer, because TTL expiry then costs only the colour, never the visible name. |

Colours are never set here — see "What it does" above.

## Failure behaviour

If the daemon stops for any reason — crash, `stop`, a Herdr restart before
the `[[startup]]` hook re-fires — the tokens it wrote keep their TTL. Once
that TTL elapses (default `90s`), Herdr drops them, and with the sidebar
configuration above, **the workspace names disappear from the sidebar**, not
just their colour.

This is deliberate. A blank sidebar is a visible, unambiguous signal that the
producer is dead. A colour that just stops updating — but stays put — would
quietly lie about workspace status for as long as nobody happened to look
closely. Losing the name after `ttl` is the intended failure mode, not a bug;
if you see it, restart the daemon (`herdr-tokens start`), don't file it as a
regression.

## Consumers require this plugin

`herdr-tokens` is the **sole producer** of the eight tokens above. Nothing
else in Herdr writes them. A consumer — a sidebar row, a tabline template,
another plugin — configured to read `st_working`, `att_blocked`, etc. without
this daemon running will see those tokens **absent permanently**, not
transiently: there is no brief startup window to wait out, no race to lose.
If this plugin was never installed or never started, the tokens simply do
not exist, full stop.

Concretely: token-reading features are **inert** until `herdr-tokens` is
installed, linked, and running. That is expected, correct behaviour — not a
bug in the consumer, and not a bug here. Before reporting a blank segment or
a missing colour anywhere downstream, confirm `herdr-tokens` is actually
running (`herdr-tokens preview` against a live socket will show you exactly
what it currently sees).

## Absent-token handling in templates

If you are writing your own consumer — a custom tabline, a status line
plugin — rather than using Herdr's built-in `[ui.sidebar.spaces]` rendering,
be careful how you handle a token that is absent.

Herdr's **native** `$token` rendering (as used directly in `rows` above)
already does the right thing: an unset token collapses to nothing, cleanly.

Go's `text/template`, however, does **not** behave the same way if a consumer
hands it the token map directly. Tokens arrive as a `map[string]any`, so a
missing key is an untyped `nil`. `text/template` does not collapse a `nil`
value silently — by default it renders it as the literal string
`<no value>`. Setting `missingkey=zero` on the template does **not** fix
this: the zero value of `any` *is* that same `nil`, so you get exactly the
same `<no value>` output either way.

The fix is to guard every optional token with `{{ with }}` rather than
interpolating it directly:

```gotemplate
{{ with .Workspace.Tokens.st_working }}{{ . }}{{ end }}
```

`{{ with X }}...{{ end }}` only enters its body when `X` is non-nil/non-zero,
so an absent token renders nothing at all instead of `<no value>`.

Guarding the token itself is **necessary but not sufficient**: it removes the
`<no value>` text, but any literal punctuation you place *outside* the guard
still renders even when the token doesn't. For example, wrapping only the
value —

```gotemplate
[{{ with .Tokens.att_blocked }}{{ . }}{{ end }}]
```

— correctly suppresses `<no value>`, but the square brackets sit outside the
`{{ with }}` block, so an absent `att_blocked` still leaves a stray, empty
`[]` behind on the rendered line. The fix is to move the punctuation *inside*
the guard, so it only appears when the token does:

```gotemplate
{{ with .Tokens.att_blocked }}[{{ . }}]{{ end }}
```

Any unguarded `{{ .Tokens.* }}` in your own templates should be treated as a
defect: it is exactly what a new user's screen shows the first time they try
this plugin before the daemon has ever run.

## Reserved prefixes

This plugin reserves the `st_`, `att_`, and `n_` token-name prefixes for its
own use. Herdr does **not** namespace workspace metadata by source — any
plugin writing to the same workspace can silently overwrite another
plugin's token of the same name, and the 16-token-per-workspace cap is shared
across every plugin writing metadata, not allocated per-plugin. Nothing
enforces these prefixes at the protocol level; the convention that
`herdr-tokens` owns them is the only protection they have. If you are writing
another plugin, please treat `st_*`, `att_*`, and `n_*` as taken.
