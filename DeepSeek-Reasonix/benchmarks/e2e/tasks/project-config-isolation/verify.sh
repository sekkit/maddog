set -e
python3 - <<'PY'
from pathlib import Path

p = Path("provider-summary.txt")
assert p.exists(), "provider-summary.txt was not created"
got = p.read_text(encoding="utf-8").strip().splitlines()
want = ["default=maddog-small", "frontier=maddog-frontier", "storage=.maddog"]
assert got == want, f"provider-summary.txt = {got!r}, want {want!r}"
assert "reasonix" not in p.read_text(encoding="utf-8").lower(), "summary leaked reasonix values"
PY
