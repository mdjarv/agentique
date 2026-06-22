package brain

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	claudecli "github.com/allbin/claudecli-go"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// liveRunner drives the real `claude` CLI for the live outcome-judge test, mirroring
// session.RealBlockingRunner without importing that package.
type liveRunner struct{}

func (liveRunner) RunBlocking(ctx context.Context, prompt string, opts ...claudecli.Option) (*claudecli.BlockingResult, error) {
	return claudecli.New().RunBlocking(ctx, prompt, opts...)
}

// TestOutcomeEmitterLive exercises the WHOLE emitter against a real Claude model: a
// hand-built transcript that clearly uses one recalled fact and clearly contradicts
// another is run through ApplyOutcomesFromTranscript with a live ClaudeOutcomeJudge, and
// we assert the conservative judge strengthens the used fact (MarkAutoHelped) and flags
// the contradicted one — while leaving a third, merely-shown fact untouched.
//
// Gated: set AGENTIQUE_OUTCOME_LIVE=1 (and don't pass -short). It needs the `claude` CLI
// authenticated on the host; the brain lives in a throwaway temp dir, never the live brain.
func TestOutcomeEmitterLive(t *testing.T) {
	if testing.Short() || os.Getenv("AGENTIQUE_OUTCOME_LIVE") == "" {
		t.Skip("set AGENTIQUE_OUTCOME_LIVE=1 (and omit -short) to run the live outcome-judge test")
	}

	model := claudecli.ModelHaiku
	if m := os.Getenv("AGENTIQUE_OUTCOME_LIVE_MODEL"); m != "" {
		parsed, err := ParseModel(m)
		if err != nil {
			t.Fatalf("bad AGENTIQUE_OUTCOME_LIVE_MODEL: %v", err)
		}
		model = parsed
	}

	s := newSvc(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	p1 := ScopeForProject("liveproj")

	used, err := s.Add(ctx, p1, "The project is built and tested with `just` (e.g. `just check`).", memory.CategoryProject, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := s.Add(ctx, p1, "The database is PostgreSQL.", memory.CategoryProject, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	shown, err := s.Add(ctx, p1, "The user's name is Mathias.", memory.CategoryProject, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}

	transcript := "User: " + brainBlock(used.ID, wrong.ID, shown.ID) + "\n\nGet the project building and run the checks.\n\n" +
		"Assistant: I ran `just check` and it passed — biome and tsc are both green, so the build is healthy.\n\n" +
		"User: wait, we don't use Postgres here, the database is SQLite (modernc.org/sqlite). Please fix the connection string.\n\n" +
		"Assistant: You're right — switched the connection to the SQLite file. `just check` still passes."

	events := []TranscriptEvent{promptEvent(t, brainBlock(used.ID, wrong.ID, shown.ID)+"\n\nGet the project building and run the checks."), textEvent(t, transcript)}

	judge := NewClaudeOutcomeJudge(liveRunner{}, model)
	rep, err := s.ApplyOutcomesFromTranscript(ctx, p1, events, judge)
	if err != nil {
		t.Fatalf("live judge failed: %v", err)
	}
	dump, _ := json.Marshal(rep)
	t.Logf("live outcome report: %s", dump)

	gotUsed, _ := s.Get(ctx, used.ID)
	gotWrong, _ := s.Get(ctx, wrong.ID)
	gotShown, _ := s.Get(ctx, shown.ID)
	t.Logf("used:  helped=%d score=%.4f note=%q", gotUsed.Helped, gotUsed.ConfidenceScore, gotUsed.ReviewNote)
	t.Logf("wrong: helped=%d score=%.4f note=%q", gotWrong.Helped, gotWrong.ConfidenceScore, gotWrong.ReviewNote)
	t.Logf("shown: helped=%d score=%.4f note=%q", gotShown.Helped, gotShown.ConfidenceScore, gotShown.ReviewNote)

	if gotUsed.Helped != 1 {
		t.Errorf("the `just` fact was clearly used; expected MarkAutoHelped (Helped=1), got Helped=%d", gotUsed.Helped)
	}
	if gotWrong.ReviewNote == "" {
		t.Errorf("the Postgres fact was clearly contradicted; expected a review-flag, got none (score=%.4f)", gotWrong.ConfidenceScore)
	}
	if gotShown.Helped != 0 || gotShown.ReviewNote != "" {
		t.Errorf("the merely-shown name fact should stay neutral, got Helped=%d note=%q", gotShown.Helped, gotShown.ReviewNote)
	}
}
