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

// A workspace reporting `tokens: null` must decode cleanly, not error, and
// must be indistinguishable at the point of use from an empty tokens
// object: len 0, and a lookup on any key returns ("", false).
func TestWorkspaceTokensNullDecodesAsEmpty(t *testing.T) {
	var ws Workspace
	if err := json.Unmarshal([]byte(`{"workspace_id":"w1","label":"a","agent_status":null,"tokens":null}`), &ws); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ws.Tokens) != 0 {
		t.Fatalf("Tokens = %v, want empty for JSON null", ws.Tokens)
	}
	if v, ok := ws.Tokens["st_working"]; ok || v != "" {
		t.Fatalf("Tokens[%q] = (%q, %v), want (\"\", false)", "st_working", v, ok)
	}
}

// A workspace payload that omits the tokens key entirely (rather than
// sending it as null) must be exactly as usable as the null case -- absence
// and null are the same "nothing here" state at the point of use.
func TestWorkspaceTokensAbsentKeyDecodesAsEmpty(t *testing.T) {
	var ws Workspace
	if err := json.Unmarshal([]byte(`{"workspace_id":"w1","label":"a","agent_status":null}`), &ws); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ws.Tokens) != 0 {
		t.Fatalf("Tokens = %v, want empty when the key is absent entirely", ws.Tokens)
	}
	if v, ok := ws.Tokens["st_working"]; ok || v != "" {
		t.Fatalf("Tokens[%q] = (%q, %v), want (\"\", false)", "st_working", v, ok)
	}
}

// The non-null case: a real tokens object decodes into a populated map with
// the expected string values.
func TestWorkspaceTokensObjectDecodes(t *testing.T) {
	var ws Workspace
	body := `{"workspace_id":"w1","label":"a","agent_status":null,"tokens":{"st_idle":"space-a","n_agents":"2"}}`
	if err := json.Unmarshal([]byte(body), &ws); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ws.Tokens) != 2 {
		t.Fatalf("len(Tokens) = %d, want 2", len(ws.Tokens))
	}
	if ws.Tokens["st_idle"] != "space-a" {
		t.Fatalf(`Tokens["st_idle"] = %q, want "space-a"`, ws.Tokens["st_idle"])
	}
	if ws.Tokens["n_agents"] != "2" {
		t.Fatalf(`Tokens["n_agents"] = %q, want "2"`, ws.Tokens["n_agents"])
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
