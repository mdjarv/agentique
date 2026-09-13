package server

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The summariser answers with the model's raw text. The clamping and the
// "an empty answer is not a summary" rule are the core's, so a second
// implementation cannot decide a day is worth nothing.
func TestTheSummarizerAnswersRawText(t *testing.T) {
	runner := &recordingRunner{answer: "  Riff finished its tests.\n"}
	summarizer := newAssistantSummarizer(runner, nil, "")

	answer, err := summarizer.Summarize(context.Background(), "fold this day")
	if err != nil {
		t.Fatalf("Summarize() = %v", err)
	}
	if answer != "Riff finished its tests." {
		t.Errorf("answer = %q, want the trimmed text", answer)
	}
	if len(runner.prompts) != 1 || runner.prompts[0] != "fold this day" {
		t.Errorf("prompts = %v", runner.prompts)
	}
	if len(runner.opts) == 0 {
		t.Error("the one-shot ran with no options: it must be one turn with no builtin tools")
	}
}

// An oversized prompt is REFUSED, never cut — the triager's rule, and here the
// consequence of cutting is worse: the answer format is the last thing in the
// prompt, and what the core does with an error is leave the day unfolded, which
// is the direction that loses nothing.
func TestTheSummarizerRefusesAnOversizedPrompt(t *testing.T) {
	runner := &recordingRunner{answer: "a day"}
	summarizer := newAssistantSummarizer(runner, nil, "")

	if _, err := summarizer.Summarize(context.Background(), strings.Repeat("x", maxSummarizePrompt+1)); err == nil {
		t.Fatal("a prompt past the cap was sent anyway")
	}
	if len(runner.prompts) != 0 {
		t.Error("the refusal still paid for a model call")
	}
}

// No runner is an error rather than a panic, and an error is an error rather
// than an empty summary: a blank answer is what the core treats as "this day did
// not fold", and inventing one here would delete a day for nothing.
func TestTheSummarizerWithoutARunnerFails(t *testing.T) {
	if _, err := newAssistantSummarizer(nil, nil, "").Summarize(context.Background(), "x"); err == nil {
		t.Error("a summariser with no runner answered")
	}
	failing := newAssistantSummarizer(&recordingRunner{err: errors.New("no CLI")}, nil, "")
	if _, err := failing.Summarize(context.Background(), "x"); err == nil {
		t.Error("a failing runner answered")
	}
}
