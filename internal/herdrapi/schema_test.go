package herdrapi_test

// schema_test.go is the black-box (herdrapi_test) test package for
// ValidateFixture. It imports internal/derive as well as internal/herdrapi
// -- something an internal (package herdrapi) test file cannot do, because
// derive already imports herdrapi and Go refuses that cycle even in tests
// unless the test package is the external "_test" one. That is why
// TestAllowedTokenKeysMirrorDerive lives here rather than next to the rest
// of schema.go.
//
// These tests replace the denylist checks formerly in fixture_test.go
// (TestFixtureContainsNoRealUsername, TestFixtureHasOnlyPlaceholderHomePaths,
// TestFixtureHasNoTildePaths, TestFixtureHasOnlyPlaceholderPaths,
// TestFixtureTitlesAreGeneric, TestFixtureHasNoLiveSessionOrTerminalIDs,
// TestFixtureIsWholeResultEnvelope). See the CHANGELOG "Known limitation"
// section and schema.go's header comment for why: those checks each scanned
// for one known-bad shape, and a scan for known-bad shapes cannot
// distinguish a leak from its own fix. The positive schema below subsumes
// every guarantee those tests provided (an unrecognised path/title/UUID/
// term_ shape anywhere in the tree still fails -- now because the field
// carrying it has a closed value rule, not because a denylist happened to
// know to look for that shape) plus the property none of them had: a field
// not named in the schema at all is *itself* a failure, closing the
// "invisible to the rule that caught the previous leak" gap that let three
// different classes of private data through in sequence.
//
// A note on the example values used below as NEGATIVE test input (values the
// schema must reject): every one is fabricated. None is a value that has
// ever appeared in a real Herdr session on any machine. Values that merely
// need to *look* live-shaped for the test to be meaningful (a hex-suffixed
// term_ id, a UUID) are deliberately invented nonsense (term_deadbeef0000,
// 11111111-2222-3333-4444-555555555555) precisely so a fabricated,
// obviously-synthetic string is never confused with something worth
// scrubbing from history a second time.

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/btj93/herdr-tokens/internal/derive"
	"github.com/btj93/herdr-tokens/internal/herdrapi"
)

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// decodeFixture returns the fixture as a generic map so a test can mutate a
// deep-copied field and re-validate, without needing a second JSON file on
// disk for every negative case.
func decodeFixture(t *testing.T, path string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(readFixture(t, path), &doc); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return doc
}

func mustEncode(t *testing.T, doc map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal mutated fixture: %v", err)
	}
	return b
}

func violationPaths(vs []herdrapi.Violation) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Path
	}
	return out
}

// --- both real fixtures must pass, unmodified -------------------------------

func TestValidateFixtureAcceptsCapturedSnapshot(t *testing.T) {
	violations, err := herdrapi.ValidateFixture(readFixture(t, "../../testdata/snapshot.json"))
	if err != nil {
		t.Fatalf("ValidateFixture: %v", err)
	}
	for _, v := range violations {
		t.Errorf("unexpected violation: %s", v)
	}
	if len(violations) != 0 {
		t.Fatalf("testdata/snapshot.json must validate cleanly against the closed schema, got %d violation(s)", len(violations))
	}
}

func TestValidateFixtureAcceptsSyntheticEdgeFixture(t *testing.T) {
	violations, err := herdrapi.ValidateFixture(readFixture(t, "../../testdata/synthetic_edge.json"))
	if err != nil {
		t.Fatalf("ValidateFixture: %v", err)
	}
	for _, v := range violations {
		t.Errorf("unexpected violation: %s", v)
	}
	if len(violations) != 0 {
		t.Fatalf("testdata/synthetic_edge.json must validate cleanly against the closed schema, got %d violation(s)", len(violations))
	}
}

// --- AllowedTokenKeys must not drift from the actual public contract -------

func TestAllowedTokenKeysMirrorDerive(t *testing.T) {
	got := append([]string(nil), herdrapi.AllowedTokenKeys...)
	want := append([]string(nil), derive.AllTokens...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("herdrapi.AllowedTokenKeys %v has drifted from derive.AllTokens %v; herdrapi cannot import derive "+
			"(derive already imports herdrapi), so AllowedTokenKeys is a hand-kept copy -- update it to match", got, want)
	}
}

// --- watched-fail cases: each must be shown failing -------------------------

// TestValidateFixtureRejectsUnknownField proves the schema is CLOSED: a
// field present in the fixture but never named in the schema for its
// enclosing object fails, and names the field, rather than being silently
// ignored the way every prior denylist-based guard would have treated it.
func TestValidateFixtureRejectsUnknownField(t *testing.T) {
	doc := decodeFixture(t, "../../testdata/snapshot.json")
	snapshot := doc["snapshot"].(map[string]any)
	agents := snapshot["agents"].([]any)
	first := agents[0].(map[string]any)
	first["client_account_slug"] = "acme-corp" // a future Herdr field this schema has never seen

	violations, err := herdrapi.ValidateFixture(mustEncode(t, doc))
	if err != nil {
		t.Fatalf("ValidateFixture: %v", err)
	}
	found := false
	for _, v := range violations {
		if strings.Contains(v.Path, "client_account_slug") {
			found = true
			if !strings.Contains(v.Reason, "unknown field") {
				t.Errorf("violation for the injected field has reason %q, want it to say unknown field", v.Reason)
			}
		}
	}
	if !found {
		t.Fatalf("expected a violation naming client_account_slug, got: %v", violationPaths(violations))
	}
}

// TestValidateFixtureRejectsLiveLookingTerminalID proves a hex-suffixed
// terminal_id -- the shape a real, live Herdr terminal_id actually has --
// fails ^term_\d{12}$. The value is fabricated (never a real terminal id on
// any machine); it only needs to be shaped like one.
func TestValidateFixtureRejectsLiveLookingTerminalID(t *testing.T) {
	doc := decodeFixture(t, "../../testdata/snapshot.json")
	snapshot := doc["snapshot"].(map[string]any)
	agents := snapshot["agents"].([]any)
	first := agents[0].(map[string]any)
	first["terminal_id"] = "term_deadbeef0000" // fabricated; hex letters violate \d{12}

	violations, err := herdrapi.ValidateFixture(mustEncode(t, doc))
	if err != nil {
		t.Fatalf("ValidateFixture: %v", err)
	}
	found := false
	for _, v := range violations {
		if strings.HasSuffix(v.Path, ".terminal_id") && strings.Contains(v.Reason, "does not match required pattern") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a terminal_id pattern violation, got: %v", violationPaths(violations))
	}
}

// TestValidateFixtureRejectsOldPlaceholderTerminalID proves the specific
// historical failure this schema exists to fix: "term_00000000abcd" was one
// sibling tool's accepted placeholder convention (hex-suffixed) and another
// tool's false-positive leak report. Under ^term_\d{12}$ it fails -- neither
// tool's convention gets a free pass merely for being "the placeholder";
// only the shape the schema actually declares does.
func TestValidateFixtureRejectsOldPlaceholderTerminalID(t *testing.T) {
	doc := decodeFixture(t, "../../testdata/snapshot.json")
	snapshot := doc["snapshot"].(map[string]any)
	agents := snapshot["agents"].([]any)
	first := agents[0].(map[string]any)
	first["terminal_id"] = "term_00000000abcd"

	violations, err := herdrapi.ValidateFixture(mustEncode(t, doc))
	if err != nil {
		t.Fatalf("ValidateFixture: %v", err)
	}
	found := false
	for _, v := range violations {
		if strings.HasSuffix(v.Path, ".terminal_id") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected term_00000000abcd (the old placeholder) to fail terminal_id validation, got: %v", violationPaths(violations))
	}
}

// TestValidateFixtureRejectsRealLookingPath proves an actual (fabricated,
// never-real) home path other than the placeholder fails. "mallory" is the
// classic synthetic third-party name and refers to no one associated with
// this repository or machine.
func TestValidateFixtureRejectsRealLookingPath(t *testing.T) {
	doc := decodeFixture(t, "../../testdata/snapshot.json")
	snapshot := doc["snapshot"].(map[string]any)
	agents := snapshot["agents"].([]any)
	first := agents[0].(map[string]any)
	first["cwd"] = "/Users/mallory/work/client-project"

	violations, err := herdrapi.ValidateFixture(mustEncode(t, doc))
	if err != nil {
		t.Fatalf("ValidateFixture: %v", err)
	}
	found := false
	for _, v := range violations {
		if strings.HasSuffix(v.Path, ".cwd") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a cwd pattern violation for a real-looking path, got: %v", violationPaths(violations))
	}
}

// TestValidateFixtureRejectsNonGenericTerminalTitle proves a task
// description or client project name in terminal_title -- the exact leak
// class that was invisible to the username and path guards -- fails.
func TestValidateFixtureRejectsNonGenericTerminalTitle(t *testing.T) {
	doc := decodeFixture(t, "../../testdata/snapshot.json")
	snapshot := doc["snapshot"].(map[string]any)
	agents := snapshot["agents"].([]any)
	// pick an agent that already carries a terminal_title in the fixture
	var target map[string]any
	for _, a := range agents {
		m := a.(map[string]any)
		if _, ok := m["terminal_title"]; ok {
			target = m
			break
		}
	}
	if target == nil {
		t.Fatal("fixture setup: no agent with a terminal_title found")
	}
	target["terminal_title"] = "orders-api: nvim"

	violations, err := herdrapi.ValidateFixture(mustEncode(t, doc))
	if err != nil {
		t.Fatalf("ValidateFixture: %v", err)
	}
	found := false
	for _, v := range violations {
		if strings.HasSuffix(v.Path, ".terminal_title") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a terminal_title violation, got: %v", violationPaths(violations))
	}
}

// TestValidateFixtureRejectsNonUUIDSessionValue rounds out the identifier
// coverage: a fabricated, schema-noncompliant UUID-shaped placeholder in
// agent_session.value must fail ^00000000-0000-4000-8000-\d{12}$.
func TestValidateFixtureRejectsNonUUIDSessionValue(t *testing.T) {
	doc := decodeFixture(t, "../../testdata/snapshot.json")
	snapshot := doc["snapshot"].(map[string]any)
	agents := snapshot["agents"].([]any)
	first := agents[0].(map[string]any)
	sess := first["agent_session"].(map[string]any)
	sess["value"] = "11111111-2222-3333-4444-555555555555" // fabricated, wrong prefix/shape

	violations, err := herdrapi.ValidateFixture(mustEncode(t, doc))
	if err != nil {
		t.Fatalf("ValidateFixture: %v", err)
	}
	found := false
	for _, v := range violations {
		if strings.HasSuffix(v.Path, ".value") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an agent_session.value violation, got: %v", violationPaths(violations))
	}
}

// --- tokens field, exercised directly since neither real fixture happens to
// carry a workspace.tokens value ---------------------------------------------

func TestTokensFieldAcceptsNullAndKnownKeys(t *testing.T) {
	body := `{
		"type": "session_snapshot",
		"snapshot": {
			"workspaces": [
				{"workspace_id": "w1", "label": "space-a", "agent_status": null, "tokens": null},
				{"workspace_id": "w2", "label": "space-b", "agent_status": "idle", "tokens": {"st_idle": "space-b", "n_agents": "2"}}
			],
			"agents": []
		}
	}`
	violations, err := herdrapi.ValidateFixture([]byte(body))
	if err != nil {
		t.Fatalf("ValidateFixture: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("expected no violations, got: %v", violationPaths(violations))
	}
}

func TestTokensFieldRejectsUnknownKey(t *testing.T) {
	body := `{
		"type": "session_snapshot",
		"snapshot": {
			"workspaces": [
				{"workspace_id": "w1", "label": "space-a", "agent_status": null, "tokens": {"st_idle": "space-a", "totally_made_up_token": "x"}}
			],
			"agents": []
		}
	}`
	violations, err := herdrapi.ValidateFixture([]byte(body))
	if err != nil {
		t.Fatalf("ValidateFixture: %v", err)
	}
	found := false
	for _, v := range violations {
		if strings.Contains(v.Path, "totally_made_up_token") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a violation naming totally_made_up_token, got: %v", violationPaths(violations))
	}
}

// --- malformed JSON is a decode error, not a violation list -----------------

func TestValidateFixtureReturnsErrorForUnparseableJSON(t *testing.T) {
	_, err := herdrapi.ValidateFixture([]byte(`{not json`))
	if err == nil {
		t.Fatal("expected an error for unparseable JSON, got nil")
	}
}
