// Package controller reconciles derived tokens against what was last written.
package controller

import (
	"context"
	"time"

	"github.com/btj93/herdr-tokens/internal/config"
	"github.com/btj93/herdr-tokens/internal/derive"
	"github.com/btj93/herdr-tokens/internal/herdrapi"
)

// Reporter is the narrow slice of the API client this package needs.
// Declared here, at the point of use, rather than exported by the client.
type Reporter interface {
	ReportWorkspaceMetadata(ctx context.Context, workspaceID, source string, tokens map[string]*string, ttlMS int64) error
}

type record struct {
	written time.Time
}

type Controller struct {
	cfg  config.Config
	rep  Reporter
	last map[string]record
}

func New(cfg config.Config, rep Reporter) *Controller {
	return &Controller{cfg: cfg, rep: rep, last: map[string]record{}}
}

// Tracked reports how many workspaces have a recorded successful write.
func (c *Controller) Tracked() int { return len(c.last) }

// Invalidate drops the entire write-skip cache, forcing the next Reconcile
// to write every workspace regardless of whether the server-observed token
// set looks unchanged and fresh.
//
// It is now REDUNDANT for correctness: Reconcile compares desired tokens
// against what Herdr itself reports for the workspace (herdrapi.Workspace.
// Tokens), not against anything this Controller remembers writing, so a
// Herdr restart, a TTL expiry, a manual clear, or another plugin clobbering
// our keys are all just "the server doesn't hold what we expect" and
// self-heal on the very next tick without needing to be told a restart may
// have happened. Invalidate and its call site (daemon.Run, on the first
// successful snapshot after one or more failures) are kept anyway: they are
// harmless -- forcing an extra rewrite when the observed set already
// matches desired costs one redundant RPC, nothing more -- and removing
// them is a separate decision from closing this defect, not a consequence
// of it.
func (c *Controller) Invalidate() {
	c.last = map[string]record{}
}

// Reconcile derives the desired tokens for every workspace and writes those
// whose SERVER-REPORTED tokens (ws.Tokens) do not already match, or whose
// last successful write is older than ttl/3.
//
// Comparing against ws.Tokens rather than a remembered copy of what this
// Controller last wrote is the whole point: this daemon's own memory of
// what it wrote can be wrong about the server's actual state (a Herdr
// restart, a TTL expiry, a manual clear, or another plugin overwriting our
// keys all leave the memory stale in the same way), and comparing against
// the snapshot's own report of current state means every one of those
// causes self-heals on the next tick with no inference about which one
// happened.
//
// The staleness clause is what keeps the TTL alive. Without it the daemon
// would either never refresh (tokens expire while healthy) or write every
// tick (each write emits workspace_metadata_updated, and pane_updated is
// already around ten events per second).
func (c *Controller) Reconcile(ctx context.Context, snap herdrapi.Snapshot, now time.Time) (int, error) {
	seen := make(map[string]bool, len(snap.Workspaces))
	written := 0
	var firstErr error

	for _, ws := range snap.Workspaces {
		seen[ws.WorkspaceID] = true
		want := derive.Desired(ws, snap.Agents, c.cfg.Value)

		if prev, ok := c.last[ws.WorkspaceID]; ok &&
			observedMatchesDesired(ws.Tokens, want) &&
			now.Sub(prev.written) < c.cfg.HeartbeatAge() {
			continue
		}

		err := c.rep.ReportWorkspaceMetadata(ctx, ws.WorkspaceID, derive.Source, want, c.cfg.TTL.Milliseconds())
		if err != nil {
			// Do not record the write and do not clear anything: a transient
			// failure must not blank the sidebar. The TTL covers a real death.
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		c.last[ws.WorkspaceID] = record{written: now}
		written++
	}

	for id := range c.last {
		if !seen[id] {
			delete(c.last, id)
		}
	}
	return written, firstErr
}

// observedMatchesDesired reports whether the tokens Herdr currently reports
// for a workspace (observed) already equal what this workspace should have
// (want), so writing again would be redundant.
//
// The two shapes differ and must not be compared naively: want
// (derive.Desired's output) is a COMPLETE map over every key in
// derive.AllTokens, where a nil value means "clear this key"; observed
// (herdrapi.Workspace.Tokens) reports ONLY keys that are actually set --
// Herdr never reports a key with an explicit null/empty value, it simply
// omits keys that aren't there (and reports a nil map, not an error, when
// there is nothing at all -- see herdrapi.Workspace's doc comment). So for
// every key in want:
//   - want[key] == nil must correspond to key being ABSENT from observed
//     (nothing to clear; if it's still there, a write is needed to clear it)
//   - want[key] != nil must correspond to key being PRESENT in observed
//     with an equal string value
//
// Ranging over want alone is sufficient and does not need a length check
// the way the old remembered-state comparison did: want's key set is always
// exactly derive.AllTokens (derive.Desired guarantees every key is present,
// nil or not), so this only ever asks about keys this plugin owns. A key
// present in observed that ISN'T in want (e.g. another plugin's own
// metadata key) is simply never looked at -- this function has no opinion
// about tokens it doesn't own.
func observedMatchesDesired(observed map[string]string, want map[string]*string) bool {
	for k, wantVal := range want {
		observedVal, present := observed[k]
		if wantVal == nil {
			if present {
				return false
			}
			continue
		}
		if !present || observedVal != *wantVal {
			return false
		}
	}
	return true
}
