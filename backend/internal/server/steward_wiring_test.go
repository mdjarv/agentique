package server

import (
	"context"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/steward"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

// The findings table holds one open row per kind and subject, keeps history
// on resolve, and lets the same condition open again afterwards.
func TestStewardStoreLifecycle(t *testing.T) {
	_, queries := testutil.SetupDB(t)
	st := stewardStore{q: queries}
	ctx := context.Background()
	f := steward.Finding{Kind: steward.KindDiskLow, Severity: steward.SeverityWarning, Remedy: steward.RemedyHand,
		Facts: map[string]any{"freeBytes": 1024}, OpenedAt: "2026-09-14T12:00:00Z"}

	if err := st.Open(ctx, f); err != nil {
		t.Fatal(err)
	}
	if err := st.Open(ctx, f); err != nil {
		t.Fatalf("a second open of the same finding must be a no-op: %v", err)
	}
	f.Remedy = steward.RemedyReclaim
	if err := st.Refresh(ctx, f); err != nil {
		t.Fatal(err)
	}
	open, err := st.OpenFindings(ctx)
	if err != nil || len(open) != 1 || open[0].Remedy != steward.RemedyReclaim || open[0].Facts["freeBytes"] != float64(1024) {
		t.Fatalf("open = %+v, %v", open, err)
	}

	f.ResolvedAt = "2026-09-14T13:00:00Z"
	if err := st.Resolve(ctx, f); err != nil {
		t.Fatal(err)
	}
	if open, _ := st.OpenFindings(ctx); len(open) != 0 {
		t.Fatalf("resolved finding still open: %+v", open)
	}
	f.ResolvedAt, f.OpenedAt = "", "2026-09-14T14:00:00Z"
	if err := st.Open(ctx, f); err != nil {
		t.Fatal(err)
	}
	if open, _ := st.OpenFindings(ctx); len(open) != 1 {
		t.Fatalf("the condition could not open again: %+v", open)
	}
}
