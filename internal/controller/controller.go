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
	tokens  map[string]*string
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
// to write every workspace regardless of whether its token set looks
// unchanged and fresh.
//
// This exists for the case a bare unchanged/fresh check cannot see: a Herdr
// restart drops Herdr's own in-memory workspace metadata, but this
// Controller's `last` map has no way to know that on its own, so without
// Invalidate the daemon would skip rewriting an unchanged-looking token set
// for up to HeartbeatAge() after Herdr comes back -- leaving the sidebar
// nameless the whole time, contrary to the "recovers within one tick"
// guarantee. Callers should invoke this on the first successful snapshot
// following one or more consecutive snapshot failures, since a Herdr
// restart always produces at least one such failure (the socket goes away).
func (c *Controller) Invalidate() {
	c.last = map[string]record{}
}

// Reconcile derives the desired tokens for every workspace and writes those
// that changed or whose last write is older than ttl/3.
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
			equalTokens(prev.tokens, want) &&
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
		c.last[ws.WorkspaceID] = record{tokens: want, written: now}
		written++
	}

	for id := range c.last {
		if !seen[id] {
			delete(c.last, id)
		}
	}
	return written, firstErr
}

func equalTokens(a, b map[string]*string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			return false
		}
		switch {
		case av == nil && bv == nil:
		case av == nil || bv == nil:
			return false
		case *av != *bv:
			return false
		}
	}
	return true
}
