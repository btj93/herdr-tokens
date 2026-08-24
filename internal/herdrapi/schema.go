package herdrapi

// This file is the POSITIVE-SCHEMA fixture guard described in the v0.1.0
// changelog's "Known limitation" section. It replaces (see fixture history
// for the superseded denylist tests) a set of checks that each scanned for
// one KNOWN-BAD SHAPE: a non-placeholder /Users/ path, a tilde path, a
// non-generic terminal_title, a UUID, a term_-prefixed identifier. A denylist
// has one structural blind spot no amount of additional rules fixes: a scan
// for known-bad shapes cannot distinguish a leak from its own fix. Two
// independently written audit tools each cleared their own repository and
// each flagged the OTHER's placeholder convention as a leak -- both were
// right about the data and wrong about the other's convention, because
// neither was asserting a shape the placeholder was required to have.
//
// ValidateFixture inverts this: it asserts that the decoded fixture conforms
// to an ALLOWLIST of permitted fields and value shapes, walked recursively
// over the whole document. The critical property is that the schema is
// CLOSED -- a field present in the fixture but not named in the schema for
// its enclosing object is itself a violation, not something silently passed
// through unexamined. A future Herdr version that adds a field anywhere in
// this tree therefore fails the build the first time a fixture containing it
// is captured, and a human must classify the new field (add a rule for it
// here) before any fixture carrying it can be committed. That failure is
// deliberate and should be mildly annoying exactly when it matters.
//
// Design rule for field strictness: fields that can carry PRIVATE data --
// identifiers, paths, titles, labels, statuses, the tokens map -- get an
// exact literal, enum, or regex check, per the table in the parked-item spec
// this file implements. Every other field (counts, indices, booleans, and
// descriptive-but-non-private strings such as a layout split's "direction"
// or an agent_session's protocol "kind") gets a TYPE check only: the closed
// field-set check is what catches an unanticipated new field; there is no
// privacy payoff in also over-constraining the *values* of fields that were
// never the leak vector, and doing so only makes the schema more brittle
// against legitimate future Herdr data.
//
// This package cannot import internal/derive (derive already imports
// herdrapi for the Workspace/Agent wire types; the reverse import would be a
// cycle), so AllowedTokenKeys below is a literal, independent copy of
// derive.AllTokens. schema_test.go cross-checks the two stay in sync from an
// external (herdrapi_test) test package that can import both.

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// Violation is one schema failure: the JSON path at which it occurred (in
// the same "$.snapshot.agents[3].terminal_id" notation the superseded
// denylist tests used) and a human-readable reason.
type Violation struct {
	Path   string
	Reason string
}

func (v Violation) String() string { return v.Path + ": " + v.Reason }

// AllowedTokenKeys mirrors derive.AllTokens (see the import-cycle note
// above). Every key of a fixture's workspace.tokens object must be one of
// these.
var AllowedTokenKeys = []string{
	"st_working", "st_blocked", "st_done", "st_idle", "st_unknown", "st_none",
	"att_blocked", "n_agents",
}

// KnownAgentKinds is the set of agent-kind strings the "agent" field (and
// agent_session.agent) is permitted to carry, empirically the kinds present
// across testdata/snapshot.json (a real capture) and testdata/synthetic_edge.json
// (hand-authored). Extend this list -- deliberately, in the same commit as a
// fixture update -- if a future capture legitimately introduces a new agent
// integration; do not widen it to a free-text string field.
var KnownAgentKinds = []string{"claude", "codex"}

var (
	reWorkspaceID = regexp.MustCompile(`^w\d+$`)

	// The spec this schema implements gives tab_id as ^w\d+:t\d+$, but the
	// real captured fixture contains tab ids like "w3:tA", "w3:tC", "w3:tD"
	// -- Herdr encodes the per-tab suffix the same base36-ish way it encodes
	// pane suffixes, not always as a decimal counter. Both real fixtures
	// must validate (a hard VERIFY requirement), so this pattern is
	// broadened to match pane_id's [A-Z0-9]+ suffix instead of \d+. This is
	// the one deliberate deviation from the literal spec text; it is a
	// broadened FIELD PATTERN, not a loosened closed-field guarantee, and it
	// only accepts the shape Herdr actually emits for this field.
	reTabID = regexp.MustCompile(`^w\d+:t[A-Z0-9]+$`)

	rePaneID = regexp.MustCompile(`^w\d+:p[A-Z0-9]+$`)

	// One pattern covers both workspace labels (space-a, space-b, ... and
	// the space-x-<suffix> form used by the hand-authored synthetic edge
	// fixture to keep its three workspaces distinguishable once their
	// workspace_id had to become schema-conformant w<N> shapes) and tab
	// labels (tab-1, tab-2, ...).
	reLabel = regexp.MustCompile(`^(space-[a-z]+|space-x-[a-z]+|tab-\d+)$`)

	rePath        = regexp.MustCompile(`^/Users/user/projects/proj\d+$`)
	reTerminalID  = regexp.MustCompile(`^term_\d{12}$`)
	reSessionUUID = regexp.MustCompile(`^00000000-0000-4000-8000-\d{12}$`)
)

var agentStatusValues = []string{"idle", "working", "blocked", "done", "unknown"}
var titleValues = []string{"shell", "agent", "nvim", ""}

// fieldSpec describes one field a container object is permitted to carry.
type fieldSpec struct {
	required bool
	check    fieldChecker
}

// fieldChecker validates one already-present field value, appending any
// violations found (at or below path) to out.
type fieldChecker func(path string, v any, out *[]Violation)

// objectSchema is the closed set of fields one "kind" of object (a
// workspace, an agent, a layout's rect, ...) is permitted to carry.
type objectSchema map[string]fieldSpec

func addf(out *[]Violation, path, format string, args ...any) {
	*out = append(*out, Violation{Path: path, Reason: fmt.Sprintf(format, args...)})
}

// validateObject applies schema to an already-type-asserted JSON object.
// Every key present that schema does not name is a violation -- this is the
// closed property. Every key schema names as required that is absent is
// also a violation.
func validateObject(path string, m map[string]any, schema objectSchema, out *[]Violation) {
	for k, v := range m {
		spec, known := schema[k]
		if !known {
			addf(out, path+"."+k, "unknown field: not present in the closed fixture schema for this object")
			continue
		}
		spec.check(path+"."+k, v, out)
	}
	for name, spec := range schema {
		if !spec.required {
			continue
		}
		if _, present := m[name]; !present {
			addf(out, path+"."+name, "required field missing")
		}
	}
}

// --- field-value checkers ---------------------------------------------------

func literalString(want string) fieldChecker {
	return func(path string, v any, out *[]Violation) {
		s, ok := v.(string)
		if !ok {
			addf(out, path, "expected the literal string %q, got %T", want, v)
			return
		}
		if s != want {
			addf(out, path, "expected the literal string %q, got %q", want, s)
		}
	}
}

func pattern(kind string, re *regexp.Regexp) fieldChecker {
	return func(path string, v any, out *[]Violation) {
		s, ok := v.(string)
		if !ok {
			addf(out, path, "expected a %s string, got %T", kind, v)
			return
		}
		if !re.MatchString(s) {
			addf(out, path, "%s %q does not match required pattern %s", kind, s, re.String())
		}
	}
}

func enumString(kind string, allowed []string) fieldChecker {
	set := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		set[a] = true
	}
	return func(path string, v any, out *[]Violation) {
		s, ok := v.(string)
		if !ok {
			addf(out, path, "expected one of %v, got %T", allowed, v)
			return
		}
		if !set[s] {
			addf(out, path, "%q is not one of the allowed %s values %v", s, kind, allowed)
		}
	}
}

// nullableEnumString allows an explicit JSON null in addition to the enum.
func nullableEnumString(kind string, allowed []string) fieldChecker {
	inner := enumString(kind, allowed)
	return func(path string, v any, out *[]Violation) {
		if v == nil {
			return
		}
		inner(path, v, out)
	}
}

func typeString() fieldChecker {
	return func(path string, v any, out *[]Violation) {
		if _, ok := v.(string); !ok {
			addf(out, path, "expected a string, got %T", v)
		}
	}
}

func typeNumber() fieldChecker {
	return func(path string, v any, out *[]Violation) {
		if _, ok := v.(float64); !ok {
			addf(out, path, "expected a number, got %T", v)
		}
	}
}

func typeBool() fieldChecker {
	return func(path string, v any, out *[]Violation) {
		if _, ok := v.(bool); !ok {
			addf(out, path, "expected a bool, got %T", v)
		}
	}
}

func object(schema objectSchema) fieldChecker {
	return func(path string, v any, out *[]Violation) {
		m, ok := v.(map[string]any)
		if !ok {
			addf(out, path, "expected an object, got %T", v)
			return
		}
		validateObject(path, m, schema, out)
	}
}

func arrayOfObjects(schema objectSchema) fieldChecker {
	return func(path string, v any, out *[]Violation) {
		arr, ok := v.([]any)
		if !ok {
			addf(out, path, "expected an array, got %T", v)
			return
		}
		for i, elem := range arr {
			elemPath := fmt.Sprintf("%s[%d]", path, i)
			m, ok := elem.(map[string]any)
			if !ok {
				addf(out, elemPath, "expected an object, got %T", elem)
				continue
			}
			validateObject(elemPath, m, schema, out)
		}
	}
}

// tokensField validates the nullable Workspace.Tokens wire value: null, or an
// object every one of whose keys is in AllowedTokenKeys and every one of
// whose values is a string (Workspace.Tokens is map[string]string).
func tokensField() fieldChecker {
	allowed := make(map[string]bool, len(AllowedTokenKeys))
	for _, k := range AllowedTokenKeys {
		allowed[k] = true
	}
	return func(path string, v any, out *[]Violation) {
		if v == nil {
			return
		}
		m, ok := v.(map[string]any)
		if !ok {
			addf(out, path, "expected null or an object, got %T", v)
			return
		}
		for k, val := range m {
			if !allowed[k] {
				addf(out, path+"."+k, "token key %q is not one of derive.AllTokens", k)
				continue
			}
			if _, ok := val.(string); !ok {
				addf(out, path+"."+k, "token value must be a string, got %T", val)
			}
		}
	}
}

// --- object kinds ------------------------------------------------------------

var agentSessionSchema = objectSchema{
	// agent_session.agent mirrors the enclosing record's own agent kind.
	"agent": {check: enumString("agent kind", KnownAgentKinds)},
	// kind/source are internal protocol descriptors ("id", "herdr:claude",
	// ...), not user data -- type-checked only, per the design rule above.
	"kind":   {check: typeString()},
	"source": {check: typeString()},
	"value":  {required: true, check: pattern("agent_session.value", reSessionUUID)},
}

var scrollSchema = objectSchema{
	"max_offset_from_bottom": {check: typeNumber()},
	"offset_from_bottom":     {check: typeNumber()},
	"viewport_rows":          {check: typeNumber()},
}

var rectSchema = objectSchema{
	"height": {check: typeNumber()},
	"width":  {check: typeNumber()},
	"x":      {check: typeNumber()},
	"y":      {check: typeNumber()},
}

var layoutPaneSchema = objectSchema{
	"focused": {check: typeBool()},
	"pane_id": {required: true, check: pattern("pane_id", rePaneID)},
	"rect":    {check: object(rectSchema)},
}

var splitSchema = objectSchema{
	// direction/id are internal layout descriptors, not user data.
	"direction": {check: typeString()},
	"id":        {check: typeString()},
	"ratio":     {check: typeNumber()},
	"rect":      {check: object(rectSchema)},
}

var layoutSchema = objectSchema{
	"area":            {check: object(rectSchema)},
	"focused_pane_id": {check: pattern("pane_id", rePaneID)},
	"panes":           {check: arrayOfObjects(layoutPaneSchema)},
	"splits":          {check: arrayOfObjects(splitSchema)},
	"tab_id":          {required: true, check: pattern("tab_id", reTabID)},
	"workspace_id":    {required: true, check: pattern("workspace_id", reWorkspaceID)},
	"zoomed":          {check: typeBool()},
}

var workspaceSchema = objectSchema{
	"workspace_id":  {required: true, check: pattern("workspace_id", reWorkspaceID)},
	"label":         {required: true, check: pattern("label", reLabel)},
	"agent_status":  {required: true, check: nullableEnumString("agent_status", agentStatusValues)},
	"active_tab_id": {check: pattern("tab_id", reTabID)},
	"number":        {check: typeNumber()},
	"pane_count":    {check: typeNumber()},
	"tab_count":     {check: typeNumber()},
	"focused":       {check: typeBool()},
	"tokens":        {check: tokensField()},
}

var agentSchema = objectSchema{
	"workspace_id":            {required: true, check: pattern("workspace_id", reWorkspaceID)},
	"agent":                   {required: true, check: enumString("agent kind", KnownAgentKinds)},
	"agent_session":           {check: object(agentSessionSchema)},
	"agent_status":            {required: true, check: nullableEnumString("agent_status", agentStatusValues)},
	"cwd":                     {check: pattern("cwd", rePath)},
	"focused":                 {check: typeBool()},
	"foreground_cwd":          {check: pattern("foreground_cwd", rePath)},
	"pane_id":                 {check: pattern("pane_id", rePaneID)},
	"revision":                {check: typeNumber()},
	"state_change_seq":        {check: typeNumber()},
	"tab_id":                  {check: pattern("tab_id", reTabID)},
	"terminal_id":             {check: pattern("terminal_id", reTerminalID)},
	"terminal_title":          {check: enumString("terminal_title", titleValues)},
	"terminal_title_stripped": {check: enumString("terminal_title_stripped", titleValues)},
}

var tabSchema = objectSchema{
	"agent_status": {required: true, check: nullableEnumString("agent_status", agentStatusValues)},
	"focused":      {check: typeBool()},
	"label":        {required: true, check: pattern("label", reLabel)},
	"number":       {check: typeNumber()},
	"pane_count":   {check: typeNumber()},
	"tab_id":       {required: true, check: pattern("tab_id", reTabID)},
	"workspace_id": {required: true, check: pattern("workspace_id", reWorkspaceID)},
}

var paneSchema = objectSchema{
	"agent":                   {check: enumString("agent kind", KnownAgentKinds)},
	"agent_session":           {check: object(agentSessionSchema)},
	"agent_status":            {required: true, check: nullableEnumString("agent_status", agentStatusValues)},
	"cwd":                     {check: pattern("cwd", rePath)},
	"focused":                 {check: typeBool()},
	"foreground_cwd":          {check: pattern("foreground_cwd", rePath)},
	"pane_id":                 {required: true, check: pattern("pane_id", rePaneID)},
	"revision":                {check: typeNumber()},
	"scroll":                  {check: object(scrollSchema)},
	"tab_id":                  {required: true, check: pattern("tab_id", reTabID)},
	"terminal_id":             {required: true, check: pattern("terminal_id", reTerminalID)},
	"terminal_title":          {check: enumString("terminal_title", titleValues)},
	"terminal_title_stripped": {check: enumString("terminal_title_stripped", titleValues)},
	"workspace_id":            {required: true, check: pattern("workspace_id", reWorkspaceID)},
}

var snapshotSchema = objectSchema{
	"workspaces":           {required: true, check: arrayOfObjects(workspaceSchema)},
	"agents":               {required: true, check: arrayOfObjects(agentSchema)},
	"tabs":                 {check: arrayOfObjects(tabSchema)},
	"panes":                {check: arrayOfObjects(paneSchema)},
	"layouts":              {check: arrayOfObjects(layoutSchema)},
	"focused_pane_id":      {check: pattern("pane_id", rePaneID)},
	"focused_tab_id":       {check: pattern("tab_id", reTabID)},
	"focused_workspace_id": {check: pattern("workspace_id", reWorkspaceID)},
	"protocol":             {check: typeNumber()},
	"version":              {check: typeString()},
}

var rootSchema = objectSchema{
	"type":     {required: true, check: literalString("session_snapshot")},
	"snapshot": {required: true, check: object(snapshotSchema)},
	// _comment is human documentation on the hand-authored synthetic edge
	// fixture only (see testdata/synthetic_edge.json). It is never populated
	// from a live capture -- scripts/sanitize.py has no code path that would
	// write it -- so it cannot carry live data, and is deliberately exempt
	// from a content pattern while still being an explicitly recognised
	// field rather than silently ignored.
	"_comment": {check: typeString()},
}

// ValidateFixture decodes data as JSON and validates it against the closed
// fixture schema, returning EVERY violation found (not just the first). A
// non-nil error means the JSON itself did not parse; an empty, nil-error
// result means the fixture is fully schema-conformant.
func ValidateFixture(data []byte) ([]Violation, error) {
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("herdrapi: decode fixture: %w", err)
	}
	m, ok := doc.(map[string]any)
	if !ok {
		return []Violation{{Path: "$", Reason: fmt.Sprintf("expected a JSON object at the document root, got %T", doc)}}, nil
	}
	var out []Violation
	validateObject("$", m, rootSchema, &out)
	return out, nil
}
