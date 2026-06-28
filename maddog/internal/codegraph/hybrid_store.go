package codegraph

type HybridStoreCheckStatus string

const (
	HybridStoreCheckPass    HybridStoreCheckStatus = "pass"
	HybridStoreCheckWarning HybridStoreCheckStatus = "warning"
	HybridStoreCheckBlocked HybridStoreCheckStatus = "blocked"
)

type HybridStoreCheck struct {
	ID     string                 `json:"id"`
	Status HybridStoreCheckStatus `json:"status"`
	Detail string                 `json:"detail,omitempty"`
}

type HybridStoreAssessment struct {
	CandidateID     string              `json:"candidateId"`
	Name            string              `json:"name"`
	DefaultEnabled  bool                `json:"defaultEnabled"`
	HardDependency  bool                `json:"hardDependency"`
	OptionalViable  bool                `json:"optionalViable"`
	Capabilities    []BackendCapability `json:"capabilities"`
	Risks           []string            `json:"risks,omitempty"`
	Checks          []HybridStoreCheck  `json:"checks,omitempty"`
	Recommendation  string              `json:"recommendation,omitempty"`
	EmbeddingPolicy string              `json:"embeddingPolicy,omitempty"`
}

func ZvecHybridStoreAssessment() HybridStoreAssessment {
	return HybridStoreAssessment{
		CandidateID:    "zvec",
		Name:           "zvec hybrid vector store",
		DefaultEnabled: true,
		HardDependency: false,
		OptionalViable: true,
		Capabilities: []BackendCapability{
			BackendCapabilityDenseVector,
			BackendCapabilitySparseVector,
			BackendCapabilityFullTextSearch,
			BackendCapabilityHybridSearch,
			BackendCapabilityWAL,
		},
		Risks: []string{
			"windows_packaging",
			"cgo_or_native_binary",
			"index_migration",
			"concurrent_writes",
			"embedding_pipeline",
		},
		Checks: []HybridStoreCheck{
			{ID: "windows_packaging", Status: HybridStoreCheckWarning, Detail: "requires a native packaging proof before any bundled Windows release"},
			{ID: "wal_required", Status: HybridStoreCheckWarning, Detail: "must prove crash-safe WAL behavior for local index updates"},
			{ID: "index_migration", Status: HybridStoreCheckWarning, Detail: "needs versioned index migration before reuse across app versions"},
			{ID: "concurrent_writes", Status: HybridStoreCheckWarning, Detail: "must serialize or reject concurrent writer sessions"},
			{ID: "embedding_pipeline", Status: HybridStoreCheckWarning, Detail: "embedding generation remains outside the default v1 request path"},
			{ID: "degraded_fallback", Status: HybridStoreCheckPass, Detail: "built-in CodeGraph remains the fallback when the hybrid store is unavailable"},
		},
		EmbeddingPolicy: "external-boundary",
		Recommendation:  "enable zvec by default for v1 code-intelligence assessment while keeping built-in CodeGraph as the degraded fallback",
	}
}

func (a HybridStoreAssessment) HasCapability(want BackendCapability) bool {
	for _, capability := range a.Capabilities {
		if capability == want {
			return true
		}
	}
	return false
}

func (a HybridStoreAssessment) HasCheck(id string) bool {
	for _, check := range a.Checks {
		if check.ID == id {
			return true
		}
	}
	return false
}
