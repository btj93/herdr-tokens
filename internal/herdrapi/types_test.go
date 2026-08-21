package herdrapi

import (
	"encoding/json"
	"testing"
)

// The payload is nested under result.snapshot. Decoding result directly
// yields a zero-valued Snapshot with no error - the bug this test exists for.
func TestDecodeResultEnvelopeIsNested(t *testing.T) {
	var res snapshotResult
	if err := json.Unmarshal(fixtureBytes(t), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Type != "session_snapshot" {
		t.Fatalf("Type = %q, want session_snapshot", res.Type)
	}
	if len(res.Snapshot.Workspaces) == 0 {
		t.Fatal("decoded zero workspaces: envelope was not unwrapped")
	}
}

// Asserted separately from the envelope so an envelope change fails loudly
// instead of silently zeroing every field.
func TestDecodedSnapshotHasNonZeroFields(t *testing.T) {
	var res snapshotResult
	if err := json.Unmarshal(fixtureBytes(t), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ws := res.Snapshot.Workspaces[0]
	if ws.WorkspaceID == "" {
		t.Fatal("WorkspaceID empty")
	}
	if ws.Label == "" {
		t.Fatal("Label empty")
	}
}

// Decoding the inner object into snapshotResult must NOT look like success.
func TestUnwrappedPayloadDoesNotDecodeAsResult(t *testing.T) {
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(fixtureBytes(t), &outer); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var res snapshotResult
	if err := json.Unmarshal(outer["snapshot"], &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(res.Snapshot.Workspaces) != 0 {
		t.Fatal("inner payload decoded as if it were the envelope")
	}
}

func TestAgentStatusIsNullable(t *testing.T) {
	var ws Workspace
	if err := json.Unmarshal([]byte(`{"workspace_id":"w1","label":"a","agent_status":null}`), &ws); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ws.AgentStatus != nil {
		t.Fatalf("AgentStatus = %v, want nil for JSON null", *ws.AgentStatus)
	}
}

func TestRPCErrorCodeIsString(t *testing.T) {
	var e RPCError
	if err := json.Unmarshal([]byte(`{"code":"tab_not_found","message":"tab nope not found"}`), &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.Code != "tab_not_found" {
		t.Fatalf("Code = %q", e.Code)
	}
}
