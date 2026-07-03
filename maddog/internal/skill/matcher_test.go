package skill

import "testing"

func TestMatcherMatchesChineseDynamicSkillText(t *testing.T) {
	home := t.TempDir()
	st := New(Options{HomeDir: home, DisableBuiltins: true})
	if err := st.Inject(Skill{
		Name:        "zh-test-helper",
		Description: "中文 测试 修复 助手",
		Body:        "Use project tests to verify the requested fix.",
	}); err != nil {
		t.Fatal(err)
	}

	match := NewMatcher(st).Match("请修复中文测试失败的问题")
	if !match.Matched || match.Skill.Name != "zh-test-helper" {
		t.Fatalf("Match = %+v, want zh-test-helper for Chinese task", match)
	}
}

func TestMatcherDoesNotDropLateAlphabetTokensBeforeMatching(t *testing.T) {
	home := t.TempDir()
	st := New(Options{HomeDir: home, DisableBuiltins: true})
	if err := st.Inject(Skill{
		Name:        "vite-helper",
		Description: "vite webpack zustand build helper",
		Body:        "Inspect frontend build configuration.",
	}); err != nil {
		t.Fatal(err)
	}
	task := "a0 a1 a2 a3 a4 a5 a6 a7 a8 a9 b0 b1 b2 b3 b4 b5 b6 b7 b8 b9 c0 c1 vite webpack"
	match := NewMatcher(st).Match(task)
	if !match.Matched || match.Skill.Name != "vite-helper" {
		t.Fatalf("Match = %+v, want late vite/webpack tokens to survive truncation", match)
	}
}

func TestMatcherRequiresMoreThanSingleWeakTokenOverlap(t *testing.T) {
	home := t.TempDir()
	st := New(Options{HomeDir: home, DisableBuiltins: true})
	if err := st.Inject(Skill{
		Name:        "sql-helper",
		Description: "SQL migration helper",
		Body:        "Review migrations.",
	}); err != nil {
		t.Fatal(err)
	}
	if got := NewMatcher(st).Match("format sql").Matched; got {
		t.Fatal("single weak token overlap should not match")
	}
}
