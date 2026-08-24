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

// An un-archived session omits the marker entirely, and BOTH names must be
// absent. The client reads an absent marker as "not archived" — which is what
// makes starting a turn un-archive a session visibly — so emitting an empty
// value would be redundant, and making the field REQUIRED would be worse: the
// generated schema mirrors these tags, and a client would then reject every
// payload from a peer that predates the rename.
func TestGitSnapshotOmitsTheMarkerWhenOpen(t *testing.T) {
	got := fieldsOf(t, GitSnapshot{SessionID: "s1"})

	if _, present := got["archivedAt"]; present {
		t.Error("archivedAt should be omitted when the session is not archived")
	}
	if _, present := got["completedAt"]; present {
		t.Error("completedAt should be omitted when the session is not archived")
	}
	// The rest of the snapshot still marshals.
	if got["sessionId"] != "s1" {
		t.Errorf("sessionId: got %v", got["sessionId"])
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
