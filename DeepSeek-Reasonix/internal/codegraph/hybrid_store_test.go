package codegraph

import "testing"

func TestZvecHybridStoreAssessmentIsOptionalAndRiskGated(t *testing.T) {
	assessment := ZvecHybridStoreAssessment()
	if assessment.CandidateID != "zvec" || assessment.DefaultEnabled || assessment.HardDependency {
		t.Fatalf("zvec must remain optional and default-off: %+v", assessment)
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
		t.Fatalf("assessment should make an explicit optional viability recommendation: %+v", assessment)
	}
}
