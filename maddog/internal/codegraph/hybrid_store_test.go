package codegraph

import "testing"

func TestZvecHybridStoreAssessmentIsOptionalAndRiskGated(t *testing.T) {
	assessment := ZvecHybridStoreAssessment()
	if assessment.CandidateID != "zvec" || !assessment.DefaultEnabled || assessment.HardDependency {
		t.Fatalf("zvec must be default-enabled for v1 without becoming a hard dependency: %+v", assessment)
	}
	for _, capability := range []BackendCapability{
		BackendCapabilityDenseVector,
		BackendCapabilitySparseVector,
		BackendCapabilityFullTextSearch,
		BackendCapabilityHybridSearch,
	} {
		if !assessment.HasCapability(capability) {
			t.Fatalf("assessment missing capability %s: %+v", capability, assessment.Capabilities)
		}
	}
	for _, check := range []string{"windows_packaging", "wal_required", "index_migration", "concurrent_writes", "embedding_pipeline", "degraded_fallback"} {
		if !assessment.HasCheck(check) {
			t.Fatalf("assessment missing check %s: %+v", check, assessment.Checks)
		}
	}
	if !assessment.OptionalViable || assessment.Recommendation == "" {
		t.Fatalf("assessment should keep an explicit fallback recommendation: %+v", assessment)
	}
}
