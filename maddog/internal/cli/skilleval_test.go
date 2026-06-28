package cli

import (
	"strings"
	"testing"

	"maddog/internal/skilleval"
)

func TestSkillEvalCLIListAndEvaluateCandidate(t *testing.T) {
	isolateCLIConfigHome(t)
	root := t.TempDir()
	store := skilleval.NewProjectStore(root)
	skill := skilleval.SkillSnapshot{
		Name:        "dynamic-docs",
		Description: "Docs helper",
		Body:        "Inspect files and draft focused docs.",
		RunAs:       "inline",
	}
	for _, task := range []string{"draft parser docs", "draft setup docs"} {
		bundle := skilleval.BuildBundle(skilleval.BundleInput{
			Task:     task,
			Source:   "test",
			Snapshot: map[string]any{"tool": "read_file"},
		})
		if err := store.WriteBundle(bundle); err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.AddCandidate(skilleval.CandidateInput{
			BundleID:   bundle.ID,
			Skill:      skill,
			Validation: skilleval.ValidationSnapshot{Valid: true},
		}); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := store.ListCandidates()
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidate setup = %+v", candidates)
	}

	listOut := captureStdout(t, func() {
		if rc := Run([]string{"skilleval", "list", "--dir", root}, "test-version"); rc != 0 {
			t.Fatalf("skilleval list rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(listOut, candidates[0].ID) || !strings.Contains(listOut, "dynamic-docs") || !strings.Contains(listOut, "pending") {
		t.Fatalf("list output missing candidate:\n%s", listOut)
	}

	evalOut := captureStdout(t, func() {
		if rc := Run([]string{"skilleval", "evaluate", "--dir", root, "--json", candidates[0].ID}, "test-version"); rc != 0 {
			t.Fatalf("skilleval evaluate rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(evalOut, `"decision": "promotable"`) || !strings.Contains(evalOut, `"candidateId": "`+candidates[0].ID+`"`) {
		t.Fatalf("evaluate output missing promotable score:\n%s", evalOut)
	}
	updated, ok, err := store.ReadCandidate(candidates[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || updated.Evaluation == nil || updated.Evaluation.Decision != string(skilleval.DecisionPromotable) {
		t.Fatalf("candidate evaluation not persisted: %+v ok=%v", updated, ok)
	}
}
