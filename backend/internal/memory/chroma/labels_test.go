package chroma

import (
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

func TestMetadataForIncludesLabels(t *testing.T) {
	// Un-labeled input must be normalized so no empty-string label reaches Chroma.
	md := metadataFor(memory.Record{
		Scope:    memory.ScopeGlobal,
		Category: memory.CategoryIdentity,
		Source:   memory.SourceHuman,
	})
	for _, k := range []string{"scope", "category", "source", "volatility", "lifecycle"} {
		v, ok := md[k]
		if !ok {
			t.Errorf("metadata missing key %q", k)
			continue
		}
		if s, _ := v.(string); s == "" {
			t.Errorf("metadata %q is empty (input not normalized)", k)
		}
	}
	if md["volatility"] != "evergreen" {
		t.Errorf("volatility=%v, want evergreen (identity category)", md["volatility"])
	}
	if md["lifecycle"] != "active" {
		t.Errorf("lifecycle=%v, want active", md["lifecycle"])
	}
}
