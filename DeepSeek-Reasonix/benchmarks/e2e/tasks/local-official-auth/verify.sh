set -e
python3 - <<'PY'
import json
from pathlib import Path

obs_path = Path("auth-fixture-observations.json")
assert obs_path.exists(), "auth-fixture-observations.json missing"
obs = json.loads(obs_path.read_text(encoding="utf-8"))
assert obs["openai_authorization"] == "Bearer openai-official-access-token", obs
assert obs["exchange_seen"] is True, obs
exchange = obs["exchange_body"]
assert exchange["grant_type"] == "urn:ietf:params:oauth:grant-type:jwt-bearer", exchange
assert exchange["assertion"] == "anthropic-local-identity-jwt", exchange
assert exchange["federation_rule_id"] == "fdrl_local", exchange
assert exchange["organization_id"] == "org_local", exchange
assert exchange["service_account_id"] == "svac_local", exchange
assert obs["anthropic_authorization"] == "Bearer anthropic-minted-local-token", obs

metrics = json.loads(Path(".run-metrics.json").read_text(encoding="utf-8"))
assert metrics["upgrade_events"] >= 1, metrics
assert metrics["tool_errors"] == 1, metrics
assert metrics["steps"] >= 2, metrics
PY
