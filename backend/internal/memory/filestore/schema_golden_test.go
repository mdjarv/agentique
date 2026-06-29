package filestore

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// updateGolden rewrites the committed frontmatter schema golden. Run
// `go test ./internal/memory/filestore/... -run TestBaselineFrontmatterSchema -update`.
var updateGolden = flag.Bool("update", false, "rewrite golden files")

// fullRecord populates every frontmatter-mapped field with a non-zero value so the
// schema golden exercises every key (no omitempty key is suppressed). Source/Confidence
// are chosen as a NormalizeConfidence fixed point (agent → inferred @ 0.8) so the
// encode→decode→encode round-trip is byte-stable. When a later task adds a frontmatter
// key, extend this fixture and regenerate with -update — the diff is the schema audit.
func fullRecord() memory.Record {
	ts := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	return memory.Record{
		ID:             "schema-full-0001",
		Scope:          memory.Scope("project:demo"),
		Text:           "Every frontmatter field is populated for the schema round-trip.",
		Category:       memory.CategoryProject,
		Source:         memory.SourceAgent,
		Pinned:         true,
		Locked:         true,
		Uses:           5,
		Helped:         2,
		Corroborations: 4,
		CreatedAt:      ts,
		UpdatedAt:      ts.Add(time.Hour),
		LastUsedAt:     ts.Add(2 * time.Hour),
		DerivedFrom:    []string{"cap-1", "cap-2"},
		Subsumed:       []memory.SubsumedSource{{Scope: memory.Scope("project:other"), Text: "merged source fact"}},
		Related:        []string{"rel-1", "rel-2"},
		Community:      7,
		Area:           "tooling",
		Evidence:       memory.EvidenceCodeVerified,
		Volatility:     memory.VolatilityEphemeral,
		Lifecycle:      memory.LifecycleSuperseded,
		Relations:      []memory.TypedRelation{{Type: memory.RelationSupersedes, Target: "rel-x"}, {Type: memory.RelationDuplicates, Target: "rel-y"}},
		Keywords:       []string{"kw-one", "kw-two"},
		LastCurated:    ts.Add(3 * time.Hour),
		CuratorNote:    "curated during the schema test",

		Confidence:      memory.ConfidenceInferred,
		ConfidenceScore: 0.8,
		ReviewNote:      "flagged for review during schema test",
	}
}

func TestBaselineFrontmatterSchema(t *testing.T) {
	got, err := encode(fullRecord())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	path := filepath.Join("testdata", "schema.golden.md")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	} else {
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read golden (run with -update to create): %v", err)
		}
		if !bytes.Equal(want, got) {
			t.Fatalf("schema golden mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
		}
	}

	// Round-trip stability: decode then re-encode must reproduce the same bytes, proving
	// the on-disk schema is a fixed point (no omitempty/normalize drift).
	back, err := decode(got)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	got2, err := encode(back)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if !bytes.Equal(got, got2) {
		t.Fatalf("round-trip not byte-stable:\n--- first ---\n%s\n--- second ---\n%s", got, got2)
	}
}
