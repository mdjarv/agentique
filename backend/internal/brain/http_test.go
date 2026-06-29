package brain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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
