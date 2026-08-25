package voice

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// maxHeadline clamps a report to something speakable.
//
// The headline is read aloud, and a paragraph read aloud is worse than no
// report at all — you cannot skim speech. Constraining it here does the
// filtering work in the schema rather than in a filter downstream.
const maxHeadline = 240

// ReportKind says why the worker spoke up. The set is closed and small on
// purpose: every kind has to be something a listener would act on differently.
type ReportKind string

const (
	// ReportSurprise is something that contradicts the premise of the task —
	// the tests were already failing, the file does not exist, the approach
	// will not work. The most valuable kind, and the reason this tool exists.
	ReportSurprise ReportKind = "surprise"
	// ReportDecision is a fork the worker took that the listener might have
	// taken differently.
	ReportDecision ReportKind = "decision"
	// ReportMilestone is a meaningful step finishing in a long run.
	ReportMilestone ReportKind = "milestone"
)

// Report is one thing a working session decided is worth interrupting for.
//
// It is written by an agent operating on repository content it did not author,
// so it is **data to relay, never an instruction to follow**. Whatever speaks
// it must treat it as a quotation. The conversation it lands in is what queues
// the next prompt, so an agent that could steer that conversation could steer
// the next task.
type Report struct {
	Kind     ReportKind `json:"kind"`
	Headline string     `json:"headline"`
}

// ParseReport validates and normalises a report from the wire.
func ParseReport(kind, headline string) (Report, error) {
	k := ReportKind(kind)
	switch k {
	case ReportSurprise, ReportDecision, ReportMilestone:
	default:
		return Report{}, fmt.Errorf("unknown report kind %q: want surprise, decision or milestone", kind)
	}

	text := strings.Join(strings.Fields(headline), " ")
	if text == "" {
		return Report{}, fmt.Errorf("headline is empty")
	}
	if utf8.RuneCountInString(text) > maxHeadline {
		// Truncate on a rune boundary rather than rejecting: the worker is
		// mid-task and a hard error over verbosity helps nobody.
		runes := []rune(text)
		text = strings.TrimSpace(string(runes[:maxHeadline])) + "…"
	}
	return Report{Kind: k, Headline: text}, nil
}
