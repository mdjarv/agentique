package session

import (
	"encoding/json"
	"testing"
)

// The `completed_at` → `archived_at` rename crossed the wire, and agentique
// clients talk to *several* servers at once — one per paired machine, each
// possibly on a different release. These tests pin the expand half of that
// migration: a peer from before the rename must still be able to read which
// sessions are archived, or every archived session on this machine reappears
// in its sidebar as open work.
//
// Delete these (and the MarshalJSON methods) only once no supported release
// predates the rename.

func fieldsOf(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestGitSnapshotCarriesLegacyArchivedAlias(t *testing.T) {
	got := fieldsOf(t, GitSnapshot{SessionID: "s1", ArchivedAt: "2026-08-24T06:00:00Z"})

	if got["archivedAt"] != "2026-08-24T06:00:00Z" {
		t.Errorf("archivedAt: got %v", got["archivedAt"])
	}
	if got["completedAt"] != "2026-08-24T06:00:00Z" {
		t.Errorf("a peer predating the rename reads completedAt: got %v", got["completedAt"])
	}
	// The rest of the snapshot must survive the custom marshaller.
	if got["sessionId"] != "s1" {
		t.Errorf("sessionId lost by MarshalJSON: got %v", got["sessionId"])
	}
	if _, ok := got["version"]; !ok {
		t.Error("version lost by MarshalJSON")
	}
}

// An un-archived session must say so explicitly. Omitting the field would be
// indistinguishable from "unchanged", and a client that just started a turn on
// an archived session would keep the row filed away.
func TestGitSnapshotAlwaysStatesArchivedAt(t *testing.T) {
	got := fieldsOf(t, GitSnapshot{SessionID: "s1"})

	v, present := got["archivedAt"]
	if !present {
		t.Fatal("archivedAt must always be present, even when empty")
	}
	if v != "" {
		t.Errorf("archivedAt: got %v, want empty", v)
	}
	// The deprecated alias keeps its historical omitempty behaviour.
	if _, present := got["completedAt"]; present {
		t.Error("completedAt should be omitted when the session is not archived")
	}
}

func TestSessionInfoCarriesLegacyArchivedAlias(t *testing.T) {
	got := fieldsOf(t, SessionInfo{ID: "s1", ArchivedAt: "2026-08-24T06:00:00Z"})

	if got["archivedAt"] != "2026-08-24T06:00:00Z" {
		t.Errorf("archivedAt: got %v", got["archivedAt"])
	}
	if got["completedAt"] != "2026-08-24T06:00:00Z" {
		t.Errorf("a peer predating the rename reads completedAt: got %v", got["completedAt"])
	}
	if got["id"] != "s1" {
		t.Errorf("id lost by MarshalJSON: got %v", got["id"])
	}
}
