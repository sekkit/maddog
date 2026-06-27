set -e
python3 - <<'PY'
import json
from pathlib import Path

data = json.loads(Path("auth-frontier-profile.json").read_text(encoding="utf-8"))
assert data["default_model"] == "small", data
assert data["small_model"] == "small", data
assert data["frontier_model"] == "frontier", data
assert data["upgrade_enabled"] is True, data
assert data["upgrade_threshold"] == 2, data
assert data["frontier_budget"] == 50000, data
assert data["advisor"]["max_uses_per_turn"] == 1, data
assert data["advisor"]["max_uses_per_session"] == 3, data
assert data["advisor"]["max_context_messages"] == 6, data
assert data["advisor"]["max_context_chars"] == 2048, data
assert data["advisor"]["native_enabled"] is True, data
assert data["advisor"]["native_max_tokens"] == 512, data
providers = {p["name"]: p for p in data["providers"]}
assert providers["small"]["kind"] == "openai"
assert providers["small"]["auth_type"] == "api_key"
assert providers["small"]["token_env"] == "ICODEEASY_API_KEY"
assert providers["small"]["models"] == ["gpt-small-a", "gpt-small-b"]
assert providers["frontier"]["kind"] == "anthropic"
assert providers["frontier"]["auth_type"] == "bearer"
assert providers["frontier"]["token_env"] == "ANTHROPIC_AUTH_TOKEN"
assert providers["official-openai"]["auth_type"] == "bearer"
assert providers["official-anthropic"]["auth_type"] == "workload_identity"
assert data["desktop_provider_access"] == ["official-openai", "official-anthropic"], data
serialized = json.dumps(data)
for forbidden in ["secret", "TOKEN_VALUE", "sk-", "Bearer "]:
    assert forbidden not in serialized, f"leaked secret-like text: {forbidden}"
PY
