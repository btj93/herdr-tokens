package derive

import (
	"testing"

	"github.com/btj93/herdr-tokens/internal/herdrapi"
)

func ptr(s string) *string { return &s }

func TestStatusTokenCoversEveryEnumValue(t *testing.T) {
	cases := map[string]string{
		"working": "st_working",
		"blocked": "st_blocked",
		"done":    "st_done",
		"idle":    "st_idle",
		"unknown": "st_unknown",
	}
	for status, want := range cases {
		if got := StatusToken(ptr(status)); got != want {
			t.Errorf("StatusToken(%q) = %q, want %q", status, got, want)
		}
	}
}

// null means NO AGENT AT ALL and must not collapse into st_unknown, which
// means "an agent is present but unclassifiable". Collapsing them paints
// every plain non-agent workspace with the something-is-wrong colour.
func TestNullStatusIsDistinctFromUnknown(t *testing.T) {
	if got := StatusToken(nil); got != "st_none" {
		t.Fatalf("StatusToken(nil) = %q, want st_none", got)
	}
	if StatusToken(nil) == StatusToken(ptr("unknown")) {
		t.Fatal("null and \"unknown\" must map to different tokens")
	}
}

func TestUnrecognisedStatusFallsBackToUnknown(t *testing.T) {
	if got := StatusToken(ptr("zombie")); got != "st_unknown" {
		t.Fatalf("StatusToken(zombie) = %q, want st_unknown", got)
	}
}

func TestDesiredSetsExactlyOneStatusTokenAndNullsTheRest(t *testing.T) {
	ws := herdrapi.Workspace{WorkspaceID: "w1", Label: "space-a", AgentStatus: ptr("working")}
	got := Desired(ws, nil, "label")

	if got["st_working"] == nil || *got["st_working"] != "space-a" {
		t.Fatalf("st_working = %v, want space-a", got["st_working"])
	}
	for _, k := range []string{"st_blocked", "st_done", "st_idle", "st_unknown", "st_none"} {
		v, present := got[k]
		if !present {
			t.Errorf("%s missing: it must be sent as an explicit null to clear it", k)
		}
		if v != nil {
			t.Errorf("%s = %v, want nil", k, *v)
		}
	}
}

func TestDesiredCarriesLabelNotStatusByDefault(t *testing.T) {
	ws := herdrapi.Workspace{WorkspaceID: "w1", Label: "space-a", AgentStatus: ptr("idle")}
	if v := Desired(ws, nil, "label")["st_idle"]; v == nil || *v != "space-a" {
		t.Fatalf("value = %v, want the workspace label", v)
	}
}

func TestDesiredStatusValueMode(t *testing.T) {
	ws := herdrapi.Workspace{WorkspaceID: "w1", Label: "space-a", AgentStatus: ptr("idle")}
	if v := Desired(ws, nil, "status")["st_idle"]; v == nil || *v != "idle" {
		t.Fatalf("value = %v, want the status string", v)
	}
}

func TestDesiredWorkspaceWithNoAgents(t *testing.T) {
	ws := herdrapi.Workspace{WorkspaceID: "w1", Label: "space-a", AgentStatus: nil}
	got := Desired(ws, nil, "label")
	if got["st_none"] == nil || *got["st_none"] != "space-a" {
		t.Fatalf("st_none = %v, want space-a", got["st_none"])
	}
	if got["n_agents"] != nil {
		t.Fatal("n_agents must be absent (nil) when there are no agents")
	}
}

func TestDesiredNeverExceedsTokenBudget(t *testing.T) {
	ws := herdrapi.Workspace{WorkspaceID: "w1", Label: "space-a", AgentStatus: ptr("blocked")}
	agents := []herdrapi.Agent{{WorkspaceID: "w1", AgentStatus: ptr("blocked")}}
	if n := len(Desired(ws, agents, "label")); n > 16 {
		t.Fatalf("emitted %d tokens, Herdr caps a workspace at 16", n)
	}
}

func TestAllTokensMatchesFrozenVocabulary(t *testing.T) {
	want := []string{
		"st_working", "st_blocked", "st_done", "st_idle", "st_unknown", "st_none",
		"att_blocked", "n_agents",
	}
	if len(AllTokens) != len(want) {
		t.Fatalf("AllTokens has %d entries, want %d", len(AllTokens), len(want))
	}
	set := map[string]bool{}
	for _, k := range AllTokens {
		set[k] = true
	}
	for _, k := range want {
		if !set[k] {
			t.Errorf("frozen token %q missing from AllTokens", k)
		}
	}
}
