// Command schema-check is the small Go entry point .githooks/pre-commit
// shells out to so it can run internal/herdrapi's closed positive fixture
// schema (see internal/herdrapi/schema.go) against a STAGED testdata/ blob,
// without duplicating the schema's rules in shell.
//
// Every violation is printed to stderr as one TAB-separated line:
//
//	SCHEMA_VIOLATION<TAB><path><TAB><json-path>: <reason>
//
// The tab separator (rather than spaces) lets a caller pull the path and
// detail apart with a plain `read` even though a real file path can contain
// spaces.
//
// Exit codes (the pre-commit hook builds this into a real binary with `go
// build` and discriminates on these directly -- see its own comments for
// why it does NOT invoke this via `go run`: `go run` collapses every
// non-zero child exit code to its own generic exit status 1 and discards the
// original value, confirmed directly against this tool, which would make
// exit 1 (a real violation) and exit 2 (this tool never got far enough to
// know) indistinguishable to a caller that only inspects the shell-visible
// exit code):
//
//	0  every argument validated with zero schema violations
//	1  at least one argument decoded as JSON but had >=1 schema violation
//	   (a POSITIVE finding -- bad fixture content, not a tooling problem);
//	   every violation is printed as a "SCHEMA_VIOLATION" line
//	2  usage error, a file could not be read, or its content did not even
//	   parse as JSON -- this tool cannot certify the file either way, so it
//	   refuses to report zero violations rather than guessing
package main

import (
	"fmt"
	"os"

	"github.com/btj93/herdr-tokens/internal/herdrapi"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: schema-check <file> [<file> ...]")
		os.Exit(2)
	}

	sawViolation := false
	for _, path := range os.Args[1:] {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "schema-check: read %s: %v\n", path, err)
			os.Exit(2)
		}
		violations, err := herdrapi.ValidateFixture(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "schema-check: %s: %v\n", path, err)
			os.Exit(2)
		}
		for _, v := range violations {
			sawViolation = true
			fmt.Fprintf(os.Stderr, "SCHEMA_VIOLATION\t%s\t%s\n", path, v)
		}
	}
	if sawViolation {
		os.Exit(1)
	}
}
