package brain

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/memory"
)

// TestToDTO_CarriesBand1Labels asserts the single mapping point (toDTO) surfaces the
// Band-1 controlled-vocabulary labels on the wire, with the JSON tag names/casing the
// hand-written frontend Memory type expects (brain-ui-spec.md F0).
func TestToDTO_CarriesBand1Labels(t *testing.T) {
	curated := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	r := memory.Record{
		ID:             "m1",
		Scope:          memory.Scope("global"),
		Text:           "the build tool is just",
		Category:       memory.Category("fact"),
		Source:         memory.SourceHuman,
		Helped:         2,
		Lifecycle:      memory.LifecycleArchived,
		Evidence:       memory.EvidenceCodeVerified,
		Volatility:     memory.VolatilityEphemeral,
		Corroborations: 3,
		Relations: []memory.TypedRelation{
			{Type: memory.RelationSupersedes, Target: "m2"},
			{Type: memory.RelationContradicts, Target: "m3"},
		},
		Keywords:    []string{"build", "tooling"},
		LastCurated: curated,
		CuratorNote: "reviewed by hand",
	}

	dto := toDTO(r)

	if dto.Lifecycle != "archived" {
		t.Errorf("Lifecycle = %q, want archived", dto.Lifecycle)
	}
	if dto.Evidence != "code_verified" {
		t.Errorf("Evidence = %q, want code_verified", dto.Evidence)
	}
	if dto.Volatility != "ephemeral" {
		t.Errorf("Volatility = %q, want ephemeral", dto.Volatility)
	}
	if dto.Corroborations != 3 {
		t.Errorf("Corroborations = %d, want 3", dto.Corroborations)
	}
	if dto.Helped != 2 {
		t.Errorf("Helped = %d, want 2", dto.Helped)
	}
	if len(dto.Relations) != 2 {
		t.Fatalf("Relations len = %d, want 2", len(dto.Relations))
	}
	if dto.Relations[0].Type != "supersedes" || dto.Relations[0].Target != "m2" {
		t.Errorf("Relations[0] = %+v, want {supersedes m2}", dto.Relations[0])
	}
	if got := strings.Join(dto.Keywords, ","); got != "build,tooling" {
		t.Errorf("Keywords = %q, want build,tooling", got)
	}
	if dto.LastCurated == nil || !dto.LastCurated.Equal(curated) {
		t.Errorf("LastCurated = %v, want %v", dto.LastCurated, curated)
	}
	if dto.CuratorNote != "reviewed by hand" {
		t.Errorf("CuratorNote = %q, want 'reviewed by hand'", dto.CuratorNote)
	}

	// JSON tag/casing contract: the frontend mirror reads these exact keys.
	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	js := string(b)
	for _, want := range []string{
		`"lifecycle":"archived"`,
		`"evidence":"code_verified"`,
		`"volatility":"ephemeral"`,
		`"corroborations":3`,
		`"relations":[{"type":"supersedes","target":"m2"}`,
		`"keywords":["build","tooling"]`,
		`"curatorNote":"reviewed by hand"`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("JSON missing %s\n got: %s", want, js)
		}
	}
}

// TestToDTO_OmitsEmptyOptionalLabels asserts a minimally-labelled (active, never-curated,
// no relations) record omits the optional fields — so the wire stays quiet for the common
// case and the frontend's optional fields read as undefined.
func TestToDTO_OmitsEmptyOptionalLabels(t *testing.T) {
	r := memory.Record{
		ID:         "m1",
		Scope:      memory.Scope("global"),
		Text:       "ordinary fact",
		Category:   memory.Category("fact"),
		Source:     memory.SourceAgent,
		Lifecycle:  memory.LifecycleActive,
		Evidence:   memory.EvidenceInferred,
		Volatility: memory.VolatilitySlow,
	}
	dto := toDTO(r)
	if dto.LastCurated != nil {
		t.Errorf("LastCurated = %v, want nil for never-curated record", dto.LastCurated)
	}
	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	js := string(b)
	// Always-set labels are present; the optional ones are omitted.
	if !strings.Contains(js, `"lifecycle":"active"`) {
		t.Errorf("JSON missing lifecycle: %s", js)
	}
	for _, absent := range []string{`"lastCurated"`, `"relations"`, `"keywords"`, `"curatorNote"`, `"corroborations"`} {
		if strings.Contains(js, absent) {
			t.Errorf("JSON should omit %s for an empty record: %s", absent, js)
		}
	}
}

// TestComputeStatusCounts is the deterministic core of the brain-health report (F6): a
// single-pass aggregation over a hand-built corpus (built directly so no load-time
// normalization perturbs the labels), one fact per tier.
func TestComputeStatusCounts(t *testing.T) {
	recs := []memory.Record{
		{Lifecycle: memory.LifecycleActive, Source: memory.SourceHuman, Evidence: memory.EvidenceUserStated, Volatility: memory.VolatilityEvergreen, Confidence: memory.ConfidenceExtracted},
		{Lifecycle: memory.LifecycleArchived, Source: memory.SourceAgent, Evidence: memory.EvidenceInferred, Volatility: memory.VolatilitySlow, Confidence: memory.ConfidenceInferred, Corroborations: 2},
		{Lifecycle: memory.LifecycleSuperseded, Source: memory.SourceConsolidated, Evidence: memory.EvidenceCorroborated, Volatility: memory.VolatilityEphemeral, Confidence: memory.ConfidenceAmbiguous, Corroborations: 3, ReviewNote: "stale"},
		{Lifecycle: memory.LifecycleActive, Source: memory.SourceCapture, Evidence: memory.EvidenceObservedOnce, Volatility: memory.VolatilitySlow, Confidence: memory.ConfidenceInferred},
	}
	c := computeStatusCounts(recs)

	if c.Total != 4 {
		t.Errorf("Total = %d, want 4", c.Total)
	}
	wantEq(t, "byLifecycle.active", c.ByLifecycle["active"], 2)
	wantEq(t, "byLifecycle.archived", c.ByLifecycle["archived"], 1)
	wantEq(t, "byLifecycle.superseded", c.ByLifecycle["superseded"], 1)
	wantEq(t, "bySource.capture", c.BySource["capture"], 1)
	wantEq(t, "bySource.human", c.BySource["human"], 1)
	wantEq(t, "byEvidence.observed_once", c.ByEvidence["observed_once"], 1)
	wantEq(t, "byEvidence.corroborated", c.ByEvidence["corroborated"], 1)
	wantEq(t, "byVolatility.slow", c.ByVolatility["slow"], 2)
	wantEq(t, "byVolatility.ephemeral", c.ByVolatility["ephemeral"], 1)
	wantEq(t, "byConfidenceTier.inferred", c.ByConfidenceTier["inferred"], 2)
	wantEq(t, "byConfidenceTier.ambiguous", c.ByConfidenceTier["ambiguous"], 1)
	wantEq(t, "reviewQueue", c.ReviewQueue, 1)
	wantEq(t, "corroboratedTotal", c.CorroboratedTotal, 5)
}

func wantEq(t *testing.T, name string, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %d, want %d", name, got, want)
	}
}

// TestHandleStatus_Counts asserts the status endpoint returns the semantic flag plus the
// counts object (the buckets that survive load-time normalization).
func TestHandleStatus_Counts(t *testing.T) {
	svc := newSvc(t)
	a := memory.New(memory.ScopeGlobal, "a live fact", memory.CategoryFact, memory.SourceHuman)
	if err := svc.store.Put(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	b := memory.New(memory.ScopeGlobal, "an archived flagged fact", memory.CategoryFact, memory.SourceAgent)
	b.Lifecycle = memory.LifecycleArchived
	b.ReviewNote = "contradicted"
	b.Corroborations = 2
	if err := svc.store.Put(t.Context(), b); err != nil {
		t.Fatal(err)
	}

	h := &Handler{Service: svc}
	mux := http.NewServeMux()
	mux.Handle("GET /api/brain/status", httperror.HandlerFunc(h.HandleStatus))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/brain/status", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var resp struct {
		Semantic bool         `json:"semantic"`
		Counts   statusCounts `json:"counts"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Semantic {
		t.Errorf("semantic = true, want false in keyword mode")
	}
	wantEq(t, "total", resp.Counts.Total, 2)
	wantEq(t, "byLifecycle.active", resp.Counts.ByLifecycle["active"], 1)
	wantEq(t, "byLifecycle.archived", resp.Counts.ByLifecycle["archived"], 1)
	wantEq(t, "bySource.human", resp.Counts.BySource["human"], 1)
	wantEq(t, "reviewQueue", resp.Counts.ReviewQueue, 1)
	wantEq(t, "corroboratedTotal", resp.Counts.CorroboratedTotal, 2)
}
