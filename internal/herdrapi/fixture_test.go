package herdrapi

import (
	"encoding/json"
	"fmt"
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

// TestFixtureContainsNoRealUsername compares the fixture against the
// username of whoever RUNS this test, not the username that was active when
// the fixture was CAPTURED. On CI the runner's account (e.g. "runner") will
// essentially never match an arbitrary contributor's real username, so this
// test passes even against a fixture that leaked someone else's name — it is
// a convenience re-check for local re-captures, not the guard that makes
// this fixture safe to publish. TestFixtureHasOnlyPlaceholderPaths below is
// the capture-machine-independent guard; do not treat this test as
// sufficient on its own.
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

// TestFixtureHasOnlyPlaceholderPaths recursively walks the ENTIRE decoded
// fixture — every map value and slice element, at any depth, regardless of
// field name — and asserts that any string beginning with /Users/ or ~/
// matches the exact placeholder shape. This is a positive shape assertion,
// not a substring check keyed to a fixed list of fields: a future Herdr
// version that adds a path anywhere in the tree (a new collection, a
// history entry, a nested struct) is caught here automatically instead of
// silently leaking until someone remembers to special-case that field. It
// also does not depend on who runs it or which machine captured the
// fixture, unlike TestFixtureContainsNoRealUsername above.
func TestFixtureHasOnlyPlaceholderPaths(t *testing.T) {
	usersRe := regexp.MustCompile(`^/Users/user/projects/proj\d+$`)

	var doc any
	if err := json.Unmarshal(fixtureBytes(t), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	var walk func(path string, node any)
	walk = func(path string, node any) {
		switch v := node.(type) {
		case map[string]any:
			for k, val := range v {
				walk(path+"."+k, val)
			}
		case []any:
			for i, val := range v {
				walk(fmt.Sprintf("%s[%d]", path, i), val)
			}
		case string:
			if strings.HasPrefix(v, "/Users/") && !usersRe.MatchString(v) {
				t.Errorf("%s = %q is a /Users/ path not in placeholder shape "+
					"/Users/user/projects/projN; re-record with scripts/capture-fixture.sh", path, v)
			}
			if strings.HasPrefix(v, "~/") && v != "~/projects/app" {
				t.Errorf("%s = %q is a tilde path not equal to the placeholder ~/projects/app; "+
					"re-record with scripts/capture-fixture.sh", path, v)
			}
		}
	}
	walk("$", doc)
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
