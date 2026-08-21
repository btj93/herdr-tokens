package herdrapi

import (
	"encoding/json"
	"os"
	"os/user"
	"regexp"
	"strings"
	"testing"
)

func fixtureBytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/snapshot.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

func TestFixtureContainsNoRealUsername(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skip("cannot determine current user")
	}
	if u.Username == "user" {
		t.Skip("current user is the placeholder")
	}
	if strings.Contains(string(fixtureBytes(t)), u.Username) {
		t.Fatalf("fixture leaks real username %q", u.Username)
	}
}

func TestFixtureHasOnlyPlaceholderHomePaths(t *testing.T) {
	re := regexp.MustCompile(`/Users/([^/"]+)`)
	for _, m := range re.FindAllStringSubmatch(string(fixtureBytes(t)), -1) {
		if m[1] != "user" {
			t.Fatalf("fixture leaks home path /Users/%s", m[1])
		}
	}
}

// Tilde-form paths evade the /Users/ rule. herdr-tabline found six of these
// in fixtures that had already passed a username-and-path sweep.
func TestFixtureHasNoTildePaths(t *testing.T) {
	re := regexp.MustCompile(`~/[A-Za-z0-9_./-]+`)
	for _, m := range re.FindAllString(string(fixtureBytes(t)), -1) {
		if m != "~/projects/app" {
			t.Fatalf("fixture leaks tilde path %q", m)
		}
	}
}

// Title fields are the leak that username and path guards cannot see: they
// carry task descriptions and client project names as free text
// ("orders-api: nvim", "Refactor the payment retry"). Measured
// on a live snapshot, 37 such values passed both other guards.
//
// This is an ALLOWLIST, not a denylist: anything not explicitly generic
// fails. A new title-bearing field in a future Herdr version therefore fails
// the build rather than silently republishing private data.
func TestFixtureTitlesAreGeneric(t *testing.T) {
	allowed := map[string]bool{"": true, "shell": true, "agent": true, "nvim": true}

	var doc struct {
		Snapshot struct {
			Panes  []map[string]any `json:"panes"`
			Agents []map[string]any `json:"agents"`
			Tabs   []map[string]any `json:"tabs"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal(fixtureBytes(t), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	check := func(coll string, recs []map[string]any) {
		for i, rec := range recs {
			for _, k := range []string{"terminal_title", "terminal_title_stripped"} {
				v, ok := rec[k].(string)
				if !ok {
					continue
				}
				if !allowed[v] {
					t.Errorf("%s[%d].%s = %q is not in the generic allowlist; "+
						"re-record with scripts/capture-fixture.sh", coll, i, k, v)
				}
			}
		}
	}
	check("panes", doc.Snapshot.Panes)
	check("agents", doc.Snapshot.Agents)
	check("tabs", doc.Snapshot.Tabs)
}

func TestFixtureIsWholeResultEnvelope(t *testing.T) {
	s := string(fixtureBytes(t))
	if !strings.Contains(s, `"type": "session_snapshot"`) {
		t.Fatal("fixture is missing the result envelope; it was captured unwrapped")
	}
	if !strings.Contains(s, `"snapshot"`) {
		t.Fatal("fixture is missing the nested snapshot key")
	}
}
