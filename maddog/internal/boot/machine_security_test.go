package boot

import (
	"path/filepath"
	"testing"
)

func TestCanonicalAdditionalRootsDoesNotOverlapForbidRead(t *testing.T) {
	base := t.TempDir()
	forbidden := filepath.Join(base, "secret")
	outside := filepath.Join(base, "..", "outside")

	got := canonicalAdditionalRoots(base, []string{forbidden, base, outside}, []string{forbidden})
	if len(got) != 1 || got[0] != filepath.Clean(outside) {
		t.Fatalf("additional roots = %v, want only non-overlapping %q", got, filepath.Clean(outside))
	}
}
