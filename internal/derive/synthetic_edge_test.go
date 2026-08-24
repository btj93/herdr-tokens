package derive

// The real captured fixture (testdata/snapshot.json) reflects whatever
// machine happened to record it: on the recording machine every workspace
// had a non-nil agent_status and no agent was ever "blocked". That means
// TestBlockedBadgeAbsentWhenNoneBlocked and the st_none branch of
// TestEveryWorkspaceGetsExactlyOneStatusToken pass against it VACUOUSLY —
// they never actually walk the code path they claim to guard.
//
// null agent_status (-> st_none) and the blocked-agent count (-> att_blocked
// with a count > 1) are two of the highest-risk behaviours in this plugin,
// so this file adds a second, HAND-AUTHORED fixture
// (testdata/synthetic_edge.json, see its own _comment field) that is
// engineered to contain the cases the real capture happens to lack, and
// tests derived from it that actually exercise those branches.

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/btj93/herdr-tokens/internal/herdrapi"
)

// loadSyntheticEdgeFixture loads the hand-authored edge-case fixture. Unlike
// loadFixture (fixture_test.go), the data here was never captured from a
// live Herdr session — it exists purely to force branches the real fixture
// cannot reach.
func loadSyntheticEdgeFixture(t *testing.T) herdrapi.Snapshot {
	t.Helper()
	b, err := os.ReadFile("../../testdata/synthetic_edge.json")
	if err != nil {
		t.Fatalf("read synthetic edge fixture: %v", err)
	}
	var res struct {
		Snapshot herdrapi.Snapshot `json:"snapshot"`
	}
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(res.Snapshot.Workspaces) == 0 {
		t.Fatal("synthetic edge fixture has no workspaces")
	}
	return res.Snapshot
}

func findWorkspace(t *testing.T, snap herdrapi.Snapshot, id string) herdrapi.Workspace {
	t.Helper()
	for _, ws := range snap.Workspaces {
		if ws.WorkspaceID == id {
			return ws
		}
	}
	t.Fatalf("synthetic edge fixture missing workspace %q", id)
	return herdrapi.Workspace{}
}

// TestSyntheticEdgeNullWorkspaceGetsStNone exercises the branch that is
// vacuously true against the real fixture: a workspace whose agent_status
// is JSON null (not the string "unknown") and which no agent references at
// all must get st_none set.
func TestSyntheticEdgeNullWorkspaceGetsStNone(t *testing.T) {
	snap := loadSyntheticEdgeFixture(t)
	ws := findWorkspace(t, snap, "w900") // label "space-x-none"
	if ws.AgentStatus != nil {
		t.Fatalf("fixture setup: workspace %q must have a null agent_status, got %q", ws.WorkspaceID, *ws.AgentStatus)
	}
	for _, a := range snap.Agents {
		if a.WorkspaceID == ws.WorkspaceID {
			t.Fatalf("fixture setup: workspace %q must have zero agents referencing it", ws.WorkspaceID)
		}
	}

	got := Desired(ws, snap.Agents, "label")
	if got["st_none"] == nil {
		t.Error("st_none must be set for a workspace with a null agent_status")
	}
}

// TestSyntheticEdgeUnknownWorkspaceGetsStUnknown proves st_unknown fires for
// the literal string "unknown", which is a genuinely different case from
// null on real-shaped data.
func TestSyntheticEdgeUnknownWorkspaceGetsStUnknown(t *testing.T) {
	snap := loadSyntheticEdgeFixture(t)
	ws := findWorkspace(t, snap, "w902") // label "space-x-unknown"
	if ws.AgentStatus == nil || *ws.AgentStatus != "unknown" {
		t.Fatalf(`fixture setup: workspace %q must have agent_status "unknown", got %v`, ws.WorkspaceID, ws.AgentStatus)
	}

	got := Desired(ws, snap.Agents, "label")
	if got["st_unknown"] == nil {
		t.Error(`st_unknown must be set for a workspace with agent_status "unknown"`)
	}
}

// TestSyntheticEdgeNullAndUnknownAreDifferentKeys is the direct check that
// the two branches above land on different tokens, on data shaped like a
// real snapshot rather than hand-built Go structs.
func TestSyntheticEdgeNullAndUnknownAreDifferentKeys(t *testing.T) {
	snap := loadSyntheticEdgeFixture(t)
	noneWs := findWorkspace(t, snap, "w900")    // label "space-x-none"
	unknownWs := findWorkspace(t, snap, "w902") // label "space-x-unknown"

	noneTok := StatusToken(noneWs.AgentStatus)
	unknownTok := StatusToken(unknownWs.AgentStatus)

	if noneTok == unknownTok {
		t.Fatalf("null and \"unknown\" agent_status must map to different tokens, both got %q", noneTok)
	}
	if noneTok != "st_none" {
		t.Errorf("null agent_status mapped to %q, want st_none", noneTok)
	}
	if unknownTok != "st_unknown" {
		t.Errorf("\"unknown\" agent_status mapped to %q, want st_unknown", unknownTok)
	}
}

// TestSyntheticEdgeBlockedCountAndAgentCount exercises att_blocked with a
// count greater than one (the real fixture has zero blocked agents anywhere)
// and confirms n_agents counts every agent in the workspace, blocked or not.
func TestSyntheticEdgeBlockedCountAndAgentCount(t *testing.T) {
	snap := loadSyntheticEdgeFixture(t)
	ws := findWorkspace(t, snap, "w901") // label "space-x-blocked"

	wantBlocked, wantTotal := 0, 0
	for _, a := range snap.Agents {
		if a.WorkspaceID != ws.WorkspaceID {
			continue
		}
		wantTotal++
		if a.AgentStatus != nil && *a.AgentStatus == "blocked" {
			wantBlocked++
		}
	}
	if wantBlocked < 2 {
		t.Fatalf("fixture setup: workspace %q must have at least 2 blocked agents, found %d", ws.WorkspaceID, wantBlocked)
	}
	if wantTotal <= wantBlocked {
		t.Fatalf("fixture setup: workspace %q must also have a non-blocked agent", ws.WorkspaceID)
	}

	got := Desired(ws, snap.Agents, "label")

	gotBlocked := got["att_blocked"]
	if gotBlocked == nil || *gotBlocked != strconv.Itoa(wantBlocked) {
		t.Errorf("att_blocked = %v, want %q", gotBlocked, strconv.Itoa(wantBlocked))
	}

	gotTotal := got["n_agents"]
	if gotTotal == nil || *gotTotal != strconv.Itoa(wantTotal) {
		t.Errorf("n_agents = %v, want %q", gotTotal, strconv.Itoa(wantTotal))
	}
}
