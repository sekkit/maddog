from pathlib import Path

text = Path("app_state.txt").read_text(encoding="utf-8").strip()
assert text == "release_status=ready", text
print("ready")
