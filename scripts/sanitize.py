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

for coll in ("panes", "agents"):
    for rec in result["snapshot"].get(coll, []):
        for k in ("cwd", "foreground_cwd"):
            if rec.get(k):
                rec[k] = "/Users/user/projects/" + project(rec[k])

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
