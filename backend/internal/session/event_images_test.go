package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

const imgTestSessionID = "6f1c1d2e-0000-4000-8000-000000000001"

func TestDetachToolResultImages_RewritesInlineImages(t *testing.T) {
	in := json.RawMessage(`{"type":"tool_result","toolId":"t1","content":[` +
		`{"type":"text","text":"shot taken"},` +
		`{"type":"image","mediaType":"image/jpeg","url":"data:image/jpeg;base64,/9j/AAAA"},` +
		`{"type":"image","url":"data:image/png;base64,iVBORw0KGgo="}]}`)

	out := detachToolResultImages(imgTestSessionID, 42, in)

	var m struct {
		Content []struct {
			Type      string `json:"type"`
			Text      string `json:"text"`
			MediaType string `json:"mediaType"`
			URL       string `json:"url"`
		} `json:"content"`
	}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(m.Content) != 3 || m.Content[0].Text != "shot taken" {
		t.Fatalf("content reshaped: %s", out)
	}
	if got, want := m.Content[1].URL, "/api/sessions/"+imgTestSessionID+"/events/42/images/1"; got != want {
		t.Errorf("url = %q, want %q", got, want)
	}
	if m.Content[1].MediaType != "image/jpeg" {
		t.Errorf("mediaType lost: %q", m.Content[1].MediaType)
	}
	if got, want := m.Content[2].URL, "/api/sessions/"+imgTestSessionID+"/events/42/images/2"; got != want {
		t.Errorf("url = %q, want %q", got, want)
	}
	if m.Content[2].MediaType != "image/png" {
		t.Errorf("mediaType not derived from the data URL: %q", m.Content[2].MediaType)
	}
	if strings.Contains(string(out), "base64") {
		t.Errorf("image bytes survived into the snapshot: %s", out)
	}
}

func TestDetachToolResultImages_LeavesTextResultsUntouched(t *testing.T) {
	in := json.RawMessage(`{"type":"tool_result","toolId":"t1","content":[{"type":"text","text":"ok"}]}`)
	out := detachToolResultImages(imgTestSessionID, 1, in)
	if string(out) != string(in) {
		t.Errorf("text-only result was rewritten: %s", out)
	}
	// A text block that merely mentions a data URL is not an image block.
	in = json.RawMessage(`{"type":"tool_result","toolId":"t1","content":[{"type":"text","text":"see \"data:foo\""}]}`)
	out = detachToolResultImages(imgTestSessionID, 1, in)
	if strings.Contains(string(out), "/events/") {
		t.Errorf("text block rewritten as an image: %s", out)
	}
}

func TestBuildTurns_DetachesPromptAttachments(t *testing.T) {
	rows := []store.SessionEvent{
		{ID: 7, SessionID: imgTestSessionID, TurnIndex: 0, Seq: 0, Type: "prompt",
			Data: `{"prompt":"look","attachments":[{"name":"a.png","mimeType":"image/png","dataUrl":"data:image/png;base64,iVBORw0KGgo="}]}`},
		{ID: 8, SessionID: imgTestSessionID, TurnIndex: 0, Seq: 1, Type: "text", Data: `{"type":"text","content":"hi"}`},
	}
	turns := buildTurns(rows)
	if len(turns) != 1 || len(turns[0].Attachments) != 1 {
		t.Fatalf("turns = %+v", turns)
	}
	if got, want := turns[0].Attachments[0].DataUrl, "/api/sessions/"+imgTestSessionID+"/events/7/images/0"; got != want {
		t.Errorf("dataUrl = %q, want %q", got, want)
	}
}

func TestTrimTurnsToBudget(t *testing.T) {
	big := json.RawMessage(`{"type":"text","content":"` + strings.Repeat("x", 600) + `"}`)
	turn := func(i int) HistoryTurn {
		return HistoryTurn{Prompt: "p", Events: []json.RawMessage{big}, TurnIndex: i}
	}
	turns := []HistoryTurn{turn(0), turn(1), turn(2), turn(3)}

	got := trimTurnsToBudget(turns, 1300)
	if len(got) != 2 || got[0].TurnIndex != 2 || got[1].TurnIndex != 3 {
		t.Errorf("kept %d turns starting at %d, want the newest two", len(got), got[0].TurnIndex)
	}

	// The newest turn is kept whole even when it alone exceeds the budget.
	got = trimTurnsToBudget(turns, 10)
	if len(got) != 1 || got[0].TurnIndex != 3 {
		t.Errorf("kept %d turns, want only the newest", len(got))
	}

	if got := trimTurnsToBudget(nil, 10); len(got) != 0 {
		t.Errorf("empty in, %d out", len(got))
	}
	if got := trimTurnsToBudget(turns, 1<<20); len(got) != 4 {
		t.Errorf("under budget: kept %d of 4", len(got))
	}
}

func seedImageEvents(t *testing.T) (*store.Queries, string, int64) {
	t.Helper()
	_, q := testutil.SetupDB(t)
	project := testutil.SeedProject(t, q, "p", t.TempDir())
	sess := testutil.SeedSession(t, q, project.ID, "stopped")
	jpeg := base64.StdEncoding.EncodeToString([]byte("\xff\xd8\xff\xe0JPEG"))
	testutil.SeedEvent(t, q, sess.ID, 0, 0, "tool_result", `{"type":"tool_result","toolId":"t1","content":[`+
		`{"type":"text","text":"shot"},`+
		`{"type":"image","mediaType":"image/jpeg","url":"data:image/jpeg;base64,`+jpeg+`"},`+
		`{"type":"image","url":"data:text/html;base64,`+base64.StdEncoding.EncodeToString([]byte("<script>1</script>"))+`"}]}`)
	rows, err := q.ListEventsBySession(context.Background(), sess.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("seed: %v (%d rows)", err, len(rows))
	}
	return q, sess.ID, rows[0].ID
}

func serveImage(h *EventImageHandler, sessionID, eventID, idx string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions/{id}/events/{eventId}/images/{idx}", h.HandleServe)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID+"/events/"+eventID+"/images/"+idx, nil)
	mux.ServeHTTP(rec, req)
	return rec
}

func TestEventImageHandler_ServesInlineImage(t *testing.T) {
	q, sid, eid := seedImageEvents(t)
	h := &EventImageHandler{Queries: q}
	rec := serveImage(h, sid, itoa(eid), "1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want immutable", cc)
	}
	if got := rec.Body.String(); got != "\xff\xd8\xff\xe0JPEG" {
		t.Errorf("body = %q", got)
	}
}

func TestEventImageHandler_NonImageTypeIsADownload(t *testing.T) {
	q, sid, eid := seedImageEvents(t)
	h := &EventImageHandler{Queries: q}
	rec := serveImage(h, sid, itoa(eid), "2")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want octet-stream for text/html", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Errorf("Content-Disposition = %q, want attachment", cd)
	}
}

func TestEventImageHandler_RefusesWhatItCannotName(t *testing.T) {
	q, sid, eid := seedImageEvents(t)
	h := &EventImageHandler{Queries: q}
	cases := []struct {
		name          string
		sid, eid, idx string
		want          int
	}{
		{"text block is not an image", sid, itoa(eid), "0", http.StatusNotFound},
		{"index past the content", sid, itoa(eid), "9", http.StatusNotFound},
		{"unknown event", sid, "999999", "1", http.StatusNotFound},
		{"event of another session", "6f1c1d2e-0000-4000-8000-0000000000ff", itoa(eid), "1", http.StatusNotFound},
		{"session id is not a uuid", "..%2F..", itoa(eid), "1", http.StatusBadRequest},
		{"event id is not a number", sid, "e7", "1", http.StatusBadRequest},
		{"negative index", sid, itoa(eid), "-1", http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := serveImage(h, c.sid, c.eid, c.idx)
			if rec.Code != c.want {
				t.Errorf("status = %d, want %d", rec.Code, c.want)
			}
		})
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
