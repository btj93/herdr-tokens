import json, re, sys, getpass

out = sys.argv[1]
doc = json.load(sys.stdin)
result = doc["result"]                     # whole result, NOT result["snapshot"]

user = getpass.getuser()
blob = json.dumps(result)
blob = blob.replace(user, "user")
blob = re.sub(r"/Users/[^/\"]+", "/Users/user", blob)
# Tilde-form paths evade the /Users/ rule entirely.
blob = re.sub(r"~/[A-Za-z0-9_./-]+", "~/projects/app", blob)
result = json.loads(blob)

names = {}
def project(path):
    base = path.rstrip("/").split("/")[-1] or "root"
    if base not in names:
        names[base] = f"proj{len(names)+1}"
    return names[base]

def scrub_paths(node):
    # Recursively replace ANY string beginning with /Users/ or ~/ with a
    # placeholder of the correct shape, at ANY depth and regardless of
    # field name. A fixed list of fields (cwd, foreground_cwd, ...) only
    # protects the fields someone remembered to name; a future Herdr field
    # that carries a path anywhere in the tree (a new collection, a history
    # entry, a nested struct) is caught here automatically instead of
    # silently leaking until the next incident. project() is reused so the
    # same input path keeps mapping to the same projN placeholder wherever
    # it recurs.
    if isinstance(node, dict):
        return {k: scrub_paths(v) for k, v in node.items()}
    if isinstance(node, list):
        return [scrub_paths(v) for v in node]
    if isinstance(node, str):
        if node.startswith("/Users/"):
            return "/Users/user/projects/" + project(node)
        if node.startswith("~/"):
            return "~/projects/app"
    return node

result = scrub_paths(result)

session_ids = {}
terminal_ids = {}

# Placeholder shapes below are deliberately CONFORMANT to the positive schema
# in internal/herdrapi/schema.go, not merely distinguishable-by-convention
# from a live value the way the old "session-N" / "term-N" placeholders were.
# That distinction is the whole point: two independently written denylist
# tools each cleared their own repository's placeholders and each flagged the
# OTHER's -- one matched any "term_" containing a hex letter and flagged the
# replacement value "term_00000000abcd", the other excluded only "term_0+"
# and flagged the placeholder "term_000000000001". A shape a scanner merely
# happens not to flag today is not the same as a shape that is REQUIRED to be
# safe. These placeholders instead satisfy the schema's own patterns
# (^term_\d{12}$ and ^00000000-0000-4000-8000-\d{12}$) directly, so "is this
# conformant" has one authoritative answer instead of one per guard.
def next_terminal_id(value):
    if value not in terminal_ids:
        terminal_ids[value] = f"term_{len(terminal_ids)+1:012d}"
    return terminal_ids[value]

def next_session_uuid(value):
    if value not in session_ids:
        session_ids[value] = f"00000000-0000-4000-8000-{len(session_ids)+1:012d}"
    return session_ids[value]

def scrub_ids(node):
    # Recursively replace every `agent_session.value` and every
    # `terminal_id`, at ANY depth, the same way scrub_paths handles paths
    # above: these are real, live session and terminal identifiers -- not
    # names or paths, so the username/path guards never touch them, but
    # they are exactly the kind of residue a fixture must not carry.
    if isinstance(node, dict):
        sess = node.get("agent_session")
        if isinstance(sess, dict) and isinstance(sess.get("value"), str):
            sess["value"] = next_session_uuid(sess["value"])
        if isinstance(node.get("terminal_id"), str):
            node["terminal_id"] = next_terminal_id(node["terminal_id"])
        for v in node.values():
            scrub_ids(v)
    elif isinstance(node, list):
        for v in node:
            scrub_ids(v)

scrub_ids(result)

# Title fields are scrubbed across EVERY collection that can carry them.
# These leak in-flight task descriptions and client project names verbatim
# ("orders-api: nvim", "Refactor the payment retry") and are
# invisible to username and path guards. The generic values below are the
# allowlist the privacy test enforces.
for coll, generic in (("panes", "shell"), ("agents", "agent"), ("tabs", "shell")):
    for rec in result["snapshot"].get(coll, []):
        for k in ("terminal_title", "terminal_title_stripped"):
            if k in rec:
                rec[k] = generic

for i, ws in enumerate(result["snapshot"].get("workspaces", [])):
    ws["label"] = f"space-{chr(ord('a')+i)}"
for i, tab in enumerate(result["snapshot"].get("tabs", [])):
    tab["label"] = f"tab-{i+1}"

with open(out, "w") as f:
    json.dump(result, f, indent=2, sort_keys=True)
    f.write("\n")
print(f"wrote {out}")
