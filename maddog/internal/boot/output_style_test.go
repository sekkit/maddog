package boot

import (
	"context"
	"strings"
	"testing"
)

func TestBuildFoldsCavemanFullIntoSystemPrompt(t *testing.T) {
	dir := robustTempDir(t)
	isolateConfigHome(t)
	t.Chdir(dir)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE SYSTEM PROMPT"
output_style = "caveman-full"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "MADDOG_TEST_KEY_UNSET"
`)

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	sys := systemMessage(ctrl.History())
	if !strings.Contains(sys, "Communication style — Caveman full") {
		t.Fatalf("caveman-full style missing from system message:\n%s", sys)
	}
	if strings.Index(sys, "BASE SYSTEM PROMPT") > strings.Index(sys, "Communication style — Caveman full") {
		t.Fatalf("caveman-full should append to the base prompt:\n%s", sys)
	}
}
