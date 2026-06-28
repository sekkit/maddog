set -e
python3 - <<'PY'
import json
from pathlib import Path

output = Path("anthropic-fixture-output.txt")
assert output.exists(), "anthropic-fixture-output.txt missing"
assert output.read_text(encoding="utf-8").strip() == "MADDOG_LOCAL_ANTHROPIC_TOOL_LOOP_OK"

metrics_path = Path(".run-metrics.json")
assert metrics_path.exists(), ".run-metrics.json missing"
metrics = json.loads(metrics_path.read_text(encoding="utf-8"))
assert metrics["tool_calls"] >= 1, metrics
assert metrics["tool_errors"] == 0, metrics
assert metrics["steps"] >= 2, metrics
assert metrics["prompt_tokens"] > 0, metrics
assert metrics["completion_tokens"] > 0, metrics
PY
