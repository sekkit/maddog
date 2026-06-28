package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/skill"
	"reasonix/internal/skilleval"
)

func TestNormalizeSkillPathDirectoryLayout(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "my-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\ndescription: x\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := normalizeSkillPath(skillDir); got != root {
		t.Fatalf("normalizeSkillPath(%q) = %q, want %q", skillDir, got, root)
	}
}

func TestSkillRootsViewCountsProjectSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	project := t.TempDir()
	root := filepath.Join(project, ".maddog", "skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "proj.md"), []byte("---\ndescription: project\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}

	roots := skillRootsView()
	want := realTestPath(root)
	for _, r := range roots {
		if realTestPath(r.Dir) == want {
			if r.Status != "ok" || r.Skills != 1 || r.Scope != "project" {
				t.Fatalf("project root view = %+v", r)
			}
			if len(r.SkillItems) != 1 || r.SkillItems[0].Name != "proj" || r.SkillItems[0].Description != "project" {
				t.Fatalf("project root skill items = %+v", r.SkillItems)
			}
			return
		}
	}
	t.Fatalf("project skill root %q not found in %+v", root, roots)
}

func TestSkillRootsViewMarksEnvConfiguredCustomRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	project := t.TempDir()
	root := filepath.Join(home, "custom-skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "custom.md"), []byte("---\ndescription: custom\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REASONIX_TEST_SKILL_ROOT", root)
	cfgPath := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("[skills]\npaths = [\"${REASONIX_TEST_SKILL_ROOT}\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}

	roots := skillRootsView()
	want := realTestPath(root)
	for _, r := range roots {
		if realTestPath(r.Dir) == want {
			if !r.Configured || r.Skills != 1 || r.Scope != "custom" {
				t.Fatalf("custom root view = %+v, want configured custom root with one skill", r)
			}
			if len(r.SkillItems) != 1 || r.SkillItems[0].Name != "custom" || r.SkillItems[0].Scope != "custom" {
				t.Fatalf("custom root skill items = %+v", r.SkillItems)
			}
			return
		}
	}
	t.Fatalf("custom skill root %q not found in %+v", root, roots)
}

func TestSkillRootsViewOmitsExcludedConventionRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	project := t.TempDir()
	root := filepath.Join(home, ".agents", "skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "noisy.md"), []byte("---\ndescription: noisy\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("[skills]\nexcluded_paths = [\"~/.agents/skills\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}

	roots := skillRootsView()
	want := realTestPath(root)
	for _, r := range roots {
		if realTestPath(r.Dir) == want {
			t.Fatalf("excluded convention root should be hidden, got %+v in %+v", r, roots)
		}
	}
}

func TestRemoveSkillPathPseudoDeletesConventionRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	path := filepath.Join(home, ".agents", "skills")
	app := NewApp()

	if err := app.RemoveSkillPath(path); err != nil {
		t.Fatalf("RemoveSkillPath: %v", err)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if len(cfg.Skills.ExcludedPaths) != 1 || realTestPath(cfg.Skills.ExcludedPaths[0]) != realTestPath(path) {
		t.Fatalf("excluded paths = %v, want %q", cfg.Skills.ExcludedPaths, path)
	}
}

func TestAddSkillPathRestoresConventionRootWithoutCustomPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	path := filepath.Join(home, ".agents", "skills")
	cfgPath := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("[skills]\nexcluded_paths = [\"~/.agents/skills\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := NewApp()

	if err := app.AddSkillPath(path); err != nil {
		t.Fatalf("AddSkillPath: %v", err)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if len(cfg.Skills.ExcludedPaths) != 0 {
		t.Fatalf("excluded paths after restore = %v, want empty", cfg.Skills.ExcludedPaths)
	}
	if len(cfg.Skills.Paths) != 0 {
		t.Fatalf("restored convention root should not become custom path: %v", cfg.Skills.Paths)
	}
}

func TestCapabilitiesIncludesDisabledSkills(t *testing.T) {
	a := NewApp()
	a.setTestCtrl(control.New(control.Options{
		Skills: []skill.Skill{
			{Name: "explore", Description: "enabled", Scope: skill.ScopeBuiltin, RunAs: skill.RunSubagent},
		},
		AllSkills: []skill.Skill{
			{Name: "explore", Description: "enabled", Scope: skill.ScopeBuiltin, RunAs: skill.RunSubagent},
			{Name: "review", Description: "disabled", Scope: skill.ScopeBuiltin, RunAs: skill.RunSubagent},
		},
	}), "")
	defer a.activeCtrl().Close()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	cfgPath := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("[skills]\ndisabled_skills = [\"review\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	view := a.Capabilities()
	states := map[string]bool{}
	for _, sk := range view.Skills {
		states[sk.Name] = sk.Enabled
	}
	if states["explore"] != true {
		t.Fatalf("explore should be enabled in capabilities: %+v", view.Skills)
	}
	enabled, ok := states["review"]
	if !ok || enabled {
		t.Fatalf("review should be disabled but present in capabilities: %+v", view.Skills)
	}
}

func TestCapabilitiesIncludesSkillCandidatesAndPromotionAudit(t *testing.T) {
	isolateDesktopUserDirs(t)
	project := robustTempDir(t)
	t.Chdir(project)
	store := skilleval.NewProjectStore(project)
	candidate := seedDesktopSkillCandidate(t, store, skilleval.DecisionPromotable)
	a := NewApp()
	a.setTestCtrl(control.New(control.Options{}), "")
	defer a.activeCtrl().Close()

	view := a.Capabilities()
	if len(view.SkillCandidates) != 1 || view.SkillCandidates[0].ID != candidate.ID || view.SkillCandidates[0].Decision != string(skilleval.DecisionPromotable) {
		t.Fatalf("skill candidates view = %+v", view.SkillCandidates)
	}

	promoted, err := a.PromoteSkillCandidate(candidate.ID)
	if err != nil {
		t.Fatalf("PromoteSkillCandidate: %v", err)
	}
	if promoted.Status != string(skilleval.CandidatePromoted) || promoted.PromotedPath == "" {
		t.Fatalf("promoted candidate view = %+v", promoted)
	}
	body, err := os.ReadFile(filepath.Join(project, ".maddog", "skills", "dynamic-docs", skill.SkillFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Inspect files and draft focused docs.") {
		t.Fatalf("promoted skill file body = %s", body)
	}
	view = a.Capabilities()
	if !hasSkillView(view.Skills, "dynamic-docs") {
		t.Fatalf("promoted skill should appear in capabilities skills: %+v", view.Skills)
	}

	if err := a.RollbackPromotedSkill(candidate.ID); err != nil {
		t.Fatalf("RollbackPromotedSkill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".maddog", "skills", "dynamic-docs", skill.SkillFile)); !os.IsNotExist(err) {
		t.Fatalf("rollback should remove promoted skill, stat err=%v", err)
	}
	rolledBack, ok, err := store.ReadCandidate(candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || rolledBack.Status != skilleval.CandidatePending {
		t.Fatalf("rolled back candidate = %+v ok=%v", rolledBack, ok)
	}
	audit, err := os.ReadFile(filepath.Join(project, ".maddog", "skilleval", "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(audit), `"action":"promote"`) || !strings.Contains(string(audit), `"action":"rollback"`) {
		t.Fatalf("audit log missing promote/rollback: %s", audit)
	}
}

func TestRejectSkillCandidateKeepsCandidateTraceable(t *testing.T) {
	isolateDesktopUserDirs(t)
	project := robustTempDir(t)
	t.Chdir(project)
	store := skilleval.NewProjectStore(project)
	candidate := seedDesktopSkillCandidate(t, store, skilleval.DecisionReviewNeeded)
	a := NewApp()
	a.setTestCtrl(control.New(control.Options{}), "")
	defer a.activeCtrl().Close()

	rejected, err := a.RejectSkillCandidate(candidate.ID, "needs more held-out bundles")
	if err != nil {
		t.Fatalf("RejectSkillCandidate: %v", err)
	}
	if rejected.Status != string(skilleval.CandidateRejected) || !strings.Contains(rejected.Reason, "held-out") {
		t.Fatalf("rejected view = %+v", rejected)
	}
	view := a.Capabilities()
	if len(view.SkillCandidates) != 1 || view.SkillCandidates[0].Status != string(skilleval.CandidateRejected) {
		t.Fatalf("rejected candidate should remain visible: %+v", view.SkillCandidates)
	}
}

func seedDesktopSkillCandidate(t *testing.T, store *skilleval.Store, decision skilleval.Decision) skilleval.Candidate {
	t.Helper()
	bundle := skilleval.BuildBundle(skilleval.BundleInput{
		Task:     "draft docs",
		Source:   "test",
		Snapshot: map[string]any{"tool": "read_file"},
	})
	if err := store.WriteBundle(bundle); err != nil {
		t.Fatal(err)
	}
	candidate, _, err := store.AddCandidate(skilleval.CandidateInput{
		BundleID: bundle.ID,
		Skill: skilleval.SkillSnapshot{
			Name:        "dynamic-docs",
			Description: "Docs helper",
			Body:        "Inspect files and draft focused docs.",
			RunAs:       string(skill.RunInline),
		},
		Validation: skilleval.ValidationSnapshot{Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateCandidateEvaluation(candidate.ID, skilleval.EvaluationSummary{
		CandidateID:       candidate.ID,
		Decision:          string(decision),
		Reason:            "test decision",
		ReplayCases:       2,
		HeldOutCases:      2,
		BaselinePassRate:  0,
		CandidatePassRate: 1,
	}); err != nil {
		t.Fatal(err)
	}
	candidate, ok, err := store.ReadCandidate(candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("candidate disappeared")
	}
	return candidate
}

func hasSkillView(skills []SkillView, name string) bool {
	for _, sk := range skills {
		if sk.Name == name {
			return true
		}
	}
	return false
}

func realTestPath(path string) string {
	if p, err := filepath.EvalSymlinks(path); err == nil {
		path = p
	}
	return config.CanonicalSkillPath(path)
}
