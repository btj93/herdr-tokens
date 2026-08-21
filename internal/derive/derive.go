// Package derive maps a Herdr snapshot to the metadata tokens this plugin
// publishes. It performs no I/O.
//
// The token names below are a FROZEN public contract shared with
// herdr-tabline, which consumes them. Renaming any of them breaks a user's
// sidebar configuration and tabline's templates.
package derive

import (
	"strconv"

	"github.com/btj93/herdr-tokens/internal/herdrapi"
)

// Source is the value written to the metadata `source` field.
const Source = "herdr-tokens"

// AllTokens is every key this plugin owns. Keys not currently applicable are
// written as explicit nulls so a stale value from a previous tick is cleared.
var AllTokens = []string{
	"st_working", "st_blocked", "st_done", "st_idle", "st_unknown", "st_none",
	"att_blocked", "n_agents",
}

var statusTokens = map[string]string{
	"working": "st_working",
	"blocked": "st_blocked",
	"done":    "st_done",
	"idle":    "st_idle",
	"unknown": "st_unknown",
}

// StatusToken maps a nullable agent_status to its token name.
//
// nil means the workspace has NO agent at all and maps to st_none.
// "unknown" means an agent is present but Herdr could not classify it, and
// maps to st_unknown. These are deliberately different: collapsing them
// would paint every ordinary non-agent workspace with the blocked-ish colour.
func StatusToken(status *string) string {
	if status == nil {
		return "st_none"
	}
	if tok, ok := statusTokens[*status]; ok {
		return tok
	}
	return "st_unknown"
}

// Desired returns the complete token map for one workspace. Every key in
// AllTokens is present; inapplicable ones carry a nil value meaning "clear".
//
// valueMode selects what the st_* token carries: "label" (default) writes the
// workspace name, so it renders in the status colour; "status" writes the
// status word instead, which is safer because TTL expiry then costs only the
// colour rather than the visible name.
func Desired(ws herdrapi.Workspace, agents []herdrapi.Agent, valueMode string) map[string]*string {
	out := make(map[string]*string, len(AllTokens))
	for _, k := range AllTokens {
		out[k] = nil
	}

	value := ws.Label
	if valueMode == "status" {
		if ws.AgentStatus == nil {
			value = "none"
		} else {
			value = *ws.AgentStatus
		}
	}
	v := value
	out[StatusToken(ws.AgentStatus)] = &v

	blocked, total := 0, 0
	for _, a := range agents {
		if a.WorkspaceID != ws.WorkspaceID {
			continue
		}
		total++
		if a.AgentStatus != nil && *a.AgentStatus == "blocked" {
			blocked++
		}
	}
	if blocked > 0 {
		s := strconv.Itoa(blocked)
		out["att_blocked"] = &s
	}
	if total > 0 {
		s := strconv.Itoa(total)
		out["n_agents"] = &s
	}
	return out
}
