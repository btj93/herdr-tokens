package derive

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/btj93/herdr-tokens/internal/herdrapi"
)

func loadFixture(t *testing.T) herdrapi.Snapshot {
	t.Helper()
	b, err := os.ReadFile("../../testdata/snapshot.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var res struct {
		Snapshot herdrapi.Snapshot `json:"snapshot"`
	}
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(res.Snapshot.Workspaces) == 0 {
		t.Fatal("fixture has no workspaces")
	}
	return res.Snapshot
}

func TestEveryWorkspaceGetsExactlyOneStatusToken(t *testing.T) {
	snap := loadFixture(t)
	for _, ws := range snap.Workspaces {
		got := Desired(ws, snap.Agents, "label")
		set := 0
		for _, k := range AllTokens {
			if k[:3] == "st_" && got[k] != nil {
				set++
			}
		}
		if set != 1 {
			t.Errorf("workspace %s has %d status tokens set, want exactly 1", ws.WorkspaceID, set)
		}
	}
}

func TestAgentCountMatchesFixtureAggregation(t *testing.T) {
	snap := loadFixture(t)
	for _, ws := range snap.Workspaces {
		want := 0
		for _, a := range snap.Agents {
			if a.WorkspaceID == ws.WorkspaceID {
				want++
			}
		}
		got := Desired(ws, snap.Agents, "label")["n_agents"]
		if want == 0 {
			if got != nil {
				t.Errorf("workspace %s: n_agents = %v, want absent", ws.WorkspaceID, *got)
			}
			continue
		}
		if got == nil || *got != strconv.Itoa(want) {
			t.Errorf("workspace %s: n_agents = %v, want %d", ws.WorkspaceID, got, want)
		}
	}
}

func TestBlockedBadgeAbsentWhenNoneBlocked(t *testing.T) {
	snap := loadFixture(t)
	for _, ws := range snap.Workspaces {
		blocked := 0
		for _, a := range snap.Agents {
			if a.WorkspaceID == ws.WorkspaceID && a.AgentStatus != nil && *a.AgentStatus == "blocked" {
				blocked++
			}
		}
		got := Desired(ws, snap.Agents, "label")["att_blocked"]
		if blocked == 0 && got != nil {
			t.Errorf("workspace %s: att_blocked = %v, want absent", ws.WorkspaceID, *got)
		}
		if blocked > 0 && (got == nil || *got != strconv.Itoa(blocked)) {
			t.Errorf("workspace %s: att_blocked = %v, want %d", ws.WorkspaceID, got, blocked)
		}
	}
}

func TestNoTokenKeyExceedsHerdrNameLimit(t *testing.T) {
	for _, k := range AllTokens {
		if len(k) > 32 || len(k) == 0 {
			t.Errorf("token %q violates Herdr's ^[A-Za-z0-9_-]{1,32}$ key limit", k)
		}
		for _, r := range k {
			ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
				(r >= '0' && r <= '9') || r == '_' || r == '-'
			if !ok {
				t.Errorf("token %q contains illegal rune %q", k, r)
			}
		}
	}
}
