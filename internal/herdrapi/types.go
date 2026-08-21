package herdrapi

// Workspace is one entry of snapshot.workspaces.
//
// AgentStatus is a pointer because Herdr reports it as nullable, and null is
// NOT the same as "unknown": null means the workspace has no agent at all,
// while "unknown" means an agent is present but could not be classified.
type Workspace struct {
	WorkspaceID string  `json:"workspace_id"`
	Label       string  `json:"label"`
	AgentStatus *string `json:"agent_status"`
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
