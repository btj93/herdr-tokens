#!/usr/bin/env bash
# The ONLY sanctioned way to record a fixture. Captures the whole `result`
# object and scrubs identifying data before it ever reaches testdata/.
#
# Deviation from the original single-file design: the sanitizer lives in
# scripts/sanitize.py rather than a `python3 - <<'PY'` heredoc embedded here.
# A heredoc attached to the same command as a pipe clobbers stdin — bash
# wires stdin to the heredoc body, not to the piped `herdr api snapshot`
# output — so the embedded-heredoc form never actually receives the
# snapshot. Splitting the script preserves the exact substitution semantics
# while giving `herdr api snapshot`'s stdout a real path into the sanitizer.
set -euo pipefail
OUT="${1:-testdata/snapshot.json}"
mkdir -p "$(dirname "$OUT")"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

herdr api snapshot | python3 "$SCRIPT_DIR/sanitize.py" "$OUT"
