package herdrapi

// Workspace is one entry of snapshot.workspaces.
//
// AgentStatus is a pointer because Herdr reports it as nullable, and null is
// NOT the same as "unknown": null means the workspace has no agent at all,
// while "unknown" means an agent is present but could not be classified.
//
// Tokens is the metadata Herdr currently holds for this workspace, i.e. the
// server's own account of what was written -- as opposed to any plugin's
// memory of what it thinks it wrote. The wire value is nullable: a
// workspace with no metadata at all reports `"tokens": null`. map[string]string
// is deliberately NOT map[string]*string here: encoding/json decodes a JSON
// null directly into a nil map with no error (this is one of the few types
// null-decodes into without a pointer), and a nil map reads exactly like an
// empty one -- len() is 0, and a lookup on any key returns ("", false) --
// so callers never need to distinguish "no tokens object" from "an empty
// tokens object". The server also only ever reports keys that are actually
// SET, never a key with an explicit null value, so there is no "explicitly
// cleared" state to represent here the way derive.Desired's *string does.
type Workspace struct {
	WorkspaceID string            `json:"workspace_id"`
	Label       string            `json:"label"`
	AgentStatus *string           `json:"agent_status"`
	Tokens      map[string]string `json:"tokens"`
}

// Agent is one entry of snapshot.agents.
type Agent struct {
	WorkspaceID string  `json:"workspace_id"`
	Agent       string  `json:"agent"`
	AgentStatus *string `json:"agent_status"`
}

// Snapshot is the payload nested inside the session.snapshot result.
type Snapshot struct {
	Workspaces []Workspace `json:"workspaces"`
	Agents     []Agent     `json:"agents"`
}

// snapshotResult is the `result` object of session.snapshot. The payload is
// NESTED under `snapshot`; decoding `result` straight into Snapshot silently
// produces zero values and an apparently empty session.
type snapshotResult struct {
	Type     string   `json:"type"`
	Snapshot Snapshot `json:"snapshot"`
}

// RPCError is the `error` object. Code is a STRING (e.g. "tab_not_found"),
// not an integer.
type RPCError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return e.Code + ": " + e.Message }
