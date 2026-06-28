set -e
python3 - <<'PY'
import json
from pathlib import Path

metrics_path = Path(".run-metrics.json")
assert metrics_path.exists(), ".run-metrics.json missing"
metrics = json.loads(metrics_path.read_text(encoding="utf-8"))
assert metrics["upgrade_events"] >= 1, metrics
assert metrics["tool_calls"] >= 3, metrics
assert metrics["tool_errors"] == 3, metrics
assert metrics["steps"] >= 4, metrics
assert metrics["prompt_tokens"] > 0, metrics
assert metrics["completion_tokens"] > 0, metrics
PY
