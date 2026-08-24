package herdrapi

import (
	"os"
	"testing"
)

// fixtureBytes is shared by types_test.go, client_test.go, and (via the
// black-box package) schema_test.go. It used to live in fixture_test.go
// alongside a set of denylist privacy checks that schema.go's closed
// positive schema now subsumes entirely -- see schema_test.go and the
// CHANGELOG "Known limitation" section for why those checks were removed
// rather than kept alongside the schema as a second, weaker mechanism.
func fixtureBytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/snapshot.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}
