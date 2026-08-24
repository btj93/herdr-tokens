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
herdr plugin link /path/to/herdr-tokens
```

`link` requires the path; it does not default to the current directory.

Herdr builds the plugin using the `[[build]]` command in
`herdr-plugin.toml` (`go build -o bin/herdr-tokens ./cmd/herdr-tokens`), and
the `[[startup]]` / `[[events]] on = "pane.created"` hooks start the daemon
on Herdr startup and whenever a new pane is created.

### If you link into an already-running Herdr, start the daemon by hand

**The startup hook does not fire on `link` or `enable`.** Verified against
Herdr 0.8.2: after linking and enabling mid-session the registry showed
`enabled: true`, the plugin log was empty, and no daemon was running. The
hooks fire on Herdr *startup* and on `pane.created` — neither of which is
"you just linked me".

So a user who installs mid-session gets a registered, enabled, completely
silent plugin and no indication why nothing appears. Either restart Herdr,
create a pane, or start it explicitly:

```
herdr plugin action invoke start --plugin herdr-tokens
```

Confirm it took, rather than assuming:

```
herdr plugin log list --plugin herdr-tokens --limit 3
```

A successful start reports its pid and the daemon's log path. That log is
where snapshot failures and reconcile errors go — if tokens are not
appearing, read it first.

`start`, `stop`, `validate-config`, and `preview` are all exposed as manifest
actions, so they can be driven from Herdr's action picker as well as the CLI.

### Consumer setup

`herdr-tokens` only writes tokens; something must be configured to *read*
them, and to *colour* them, for anything to visibly change. The most common
consumer is Herdr's own sidebar, and this is also where the plugin's entire
reason for existing lives: Herdr's per-token style (`fg`/`bold`/`dim`) is
**static** — a given token is always rendered the same way, there is no
conditional styling anywhere in Herdr's UI config. The only way to get a
colour that reacts to agent status is the indirection this plugin provides:
it varies which token *name* is set, and you give each name its own,
otherwise-static, `fg`. A colour that never changes, attached to a name that
does, behaves like a conditional one.

Add this to your own `~/.config/herdr/config.toml`:

```toml
[ui.sidebar.spaces]
rows = [[
  "state_icon",
  { token = "$st_working", fg = "#dbb651" },
  { token = "$st_blocked", fg = "#e75a7c" },
  { token = "$st_done",    fg = "#8fb573" },
  { token = "$st_idle",    fg = "#888986" },
  { token = "$st_unknown", fg = "#5b5e5a" },
  { token = "$st_none",    fg = "#888986" },
]]
```

Those specific hex values are a working example, not a requirement — pick
whatever colours suit your own theme, entirely your call. What is **not**
optional is that every variant carries its **own** `fg`: that per-name
colour *is* the mechanism. A row that lists the six `$st_*` tokens as bare
strings with no `fg` at all (as an earlier draft of this README did) is
valid Herdr syntax and will render the workspace name — but every variant
renders in the same, unstyled colour, which looks and behaves exactly like
the plugin doing nothing. Herdr's inline token style block accepts `fg`
(strict `#RGB`/`#RRGGBB`), `bold`, and `dim` — nothing else, and none of it
is set by this plugin (see "What it does" above); it only decides which of
the six names is live.

Three things about this row are worth reading together:

- **Exactly one of the six is set at any moment, so exactly one `fg` ever
  shows.** `st_working`, `st_blocked`, `st_done`, `st_idle`, `st_unknown`,
  `st_none` are a mutually exclusive group per workspace (see Token
  reference below) — one token carries the name, the rest are cleared, so
  the row's per-token `fg` values never compete or blend, only ever swap.
- **You must list all six variants, including `$st_none`.** If you leave
  `$st_none` out of the row because it "does nothing" — it's the variant
  that fires on every ordinary workspace with no agent running, so it is
  also the one variant nobody happens to test by hand — then the row has
  nothing to render for every agent-free workspace, and **the workspace's
  name disappears from the sidebar entirely**, not just its colour. The name
  is carried *inside* the token (see Configuration below), not displayed
  alongside it, so there is no fallback text left once the token is absent.
- **Do not also include a plain `workspace` token in the same row.** The
  default sidebar row ships as `rows = [["state_icon", "workspace"]]`. If you
  keep `"workspace"` alongside the six `st_*` entries, the workspace name
  renders twice: once from the plain built-in token, once from whichever
  `st_*` token happens to be set. Replace `"workspace"` with the six styled
  `st_*` entries; don't add to it.

#### Styling `att_blocked` and `n_agents`

If you also surface the two count tokens — as extra columns in the same row,
or a second row — give them the same styled treatment, not bare tokens, for
the same reason as above. A blocked agent is the one state worth actually
calling attention to, so `bold` earns its keep here:

```toml
[ui.sidebar.spaces]
rows = [[
  "state_icon",
  { token = "$st_working", fg = "#dbb651" },
  { token = "$st_blocked", fg = "#e75a7c" },
  { token = "$st_done",    fg = "#8fb573" },
  { token = "$st_idle",    fg = "#888986" },
  { token = "$st_unknown", fg = "#5b5e5a" },
  { token = "$st_none",    fg = "#888986" },
  { token = "$att_blocked", fg = "#e75a7c", bold = true },
  { token = "$n_agents",    fg = "#888986", dim = true },
]]
```

Both are absent — not `"0"` — when they don't apply (see Token reference
below), so a bare `$att_blocked` and this styled one render identically
until an agent is actually blocked. Style it anyway: the point of this
plugin is that the name/colour it produces should never be the *one* place
in your config that's still a static, unconditional token.

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

**Config changes require a restart.** `config.toml` is read once, at daemon
startup, not watched or hot-reloaded. Editing it while the daemon is already
running has no effect on that running daemon; the new values take effect only
on the next `herdr-tokens stop && herdr-tokens start` (or the equivalent
`stop`/`start` plugin actions from Herdr's action picker).

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

## Contributing / working on fixtures

`testdata/` fixtures are captures of a real Herdr session and are the single
easiest way for this repo to leak someone's real username, filesystem
layout, or window titles. That risk is handled in three layers, cheapest and
earliest first:

1. **Sanitize at capture.** `scripts/capture-fixture.sh` (`make fixture`) is
   the *only* sanctioned way to produce or refresh a fixture. It pipes a live
   snapshot through `scripts/sanitize.py` before anything touches
   `testdata/`, so an unsanitized capture should never even reach the
   working tree.
2. **The pre-commit hook blocks the commit.** Run `make install-hooks` once
   per clone (this points git at the tracked `.githooks/` directory, since
   git does not share `.git/hooks/` across clones — a plain `git clone`
   without this step has no hook installed). From then on, `git commit`
   inspects the *staged* contents of any `testdata/` file — not the working
   tree, so staging a bad fixture and then cleaning up the file on disk
   afterwards still gets caught — and refuses the commit if it fails
   `internal/herdrapi/schema.go`'s **closed positive schema**: every field
   present in the fixture must be one the schema names, with the exact shape
   (a literal, an enum, or a regex — e.g. `terminal_id` must match
   `^term_\d{12}$`, `cwd`/`foreground_cwd` must match
   `^/Users/user/projects/proj\d+$`) that field is declared to have. A field
   the schema has never seen at all is refused too, by design — see "Why a
   schema, not a denylist" below. The hook also separately blocks your own
   machine account name (`id -un`) appearing anywhere in the staged blob,
   since a couple of schema fields are deliberately type-checked only (not
   pattern-constrained) and a leaked account name could otherwise hide in
   one of those. This is what actually stops a leak: once bad data is
   committed *locally*, a push is one command away, and exposure becomes a
   matter of when, not if.

### Why a schema, not a denylist

Earlier versions of this guard (still visible in git history) scanned staged
fixtures for a fixed list of *known-bad shapes*: a `/Users/` path that wasn't
the placeholder, a tilde path, a non-generic `terminal_title`, a UUID, a
`term_`-prefixed identifier. That is a **denylist**, and it has one
structural blind spot no amount of additional rules fixes: **a scan for
known-bad shapes cannot distinguish a leak from its own fix.** This was
demonstrated, not theorised — see the CHANGELOG's "Fixture privacy guard —
replaced the shape-based denylist with a closed positive schema" entry for
the full account of two independently written audit tools that each cleared
their own repository and each flagged the *other's* placeholder convention
as a leak.

`internal/herdrapi/schema.go` inverts this: it asserts the decoded fixture
conforms to an **allowlist** of permitted fields and value shapes, walked
over the whole document. The schema is **closed** — a field present in a
fixture but not named in the schema for its enclosing object fails, naming
the field, rather than passing through unexamined. A future Herdr version
that adds a field anywhere in this tree fails the build the first time a
fixture containing it is captured, until a human classifies that field here.
That failure is deliberate, and should be mildly annoying exactly when it
matters. Run it directly with `go test ./internal/herdrapi/... -run
'^Test(ValidateFixture|TokensField|AllowedTokenKeysMirrorDerive)' -v`.
3. **CI is the backstop, not the guard.** The `fixtures-are-sanitized` job in
   `.github/workflows/ci.yml` runs the same checks again. By the time it
   runs, a real leak is already pushed — public, indexed, possibly cloned —
   and the only remedy left is a history rewrite (a sibling project had to
   do exactly this, across 14 commits, after a fixture with a real client
   name slipped through). CI exists only to catch a commit made with
   `--no-verify`, or a clone that skipped `make install-hooks` — it is the
   last line of defence, not the first, and should never be treated as a
   reason the hook is redundant.

**Never hand-edit a fixture** to patch a flagged line. It isn't a fix: the
next capture silently overwrites the hand edit anyway, and in the meantime
you've likely just moved the same real data somewhere the checks don't
happen to look. Re-run `scripts/capture-fixture.sh` instead.
