package voice

import (
	"sort"
	"strings"
	"unicode"
)

// Matching a spoken session name is not string equality, and it is not fuzzy
// search either. It is one specific problem: a name that went through a
// microphone, a speech model, and a person who half-remembers it.
//
// So "live voice dialogue" has to find "Live Voice Dialog", "the agentique one"
// has to find a session by its project, and nothing may ever be picked
// automatically — the caller cannot see what was chosen, and starting work in
// the wrong session is the one mistake this feature must not make.

const (
	// matchFloor is the score below which a row is not worth offering. Low, so
	// a mangled transcription still surfaces its candidate, and paired with
	// never auto-picking rather than with a confident cut-off.
	matchFloor = 0.34

	// clearMargin is how far ahead the top candidate must be before the
	// assistant may treat it as the obvious one — and even then it confirms by
	// name. Below this it has to ask.
	clearMargin = 0.25

	// clearScore is how good the top candidate must be on its own terms. A
	// clear winner among bad matches is still a bad match.
	clearScore = 0.6

	// maxCandidates is how many candidates come back. Five is already more than
	// anyone wants read aloud; the assistant is expected to name two or three.
	maxCandidates = 5
)

// Field weights. A session's own name is what someone says; the project and
// machine are how they narrow it down when several sessions share a name.
const (
	weightName    = 1.0
	weightProject = 0.62
	weightMachine = 0.45
)

// Match strengths for one query token against one field token.
const (
	scoreExact  = 1.0
	scorePrefix = 0.82 // "dialog" / "dialogue", "recon" / "reconnect"
	scoreInside = 0.55 // the token appears inside a longer word
)

// Candidate is one possible answer to a spoken name, with the score that put it
// there. The score is not read aloud; it decides ordering and whether the top
// one is clear enough to confirm rather than ask about.
type Candidate struct {
	Row   SessionRow
	Score float64
}

// MatchSessions ranks rows against a spoken query.
//
// Ranking is match quality first, then how much the session is demanding of the
// operator, then recency — because among equally good matches the one waiting
// for an answer is the one they probably meant.
//
// It never picks. The second return says only whether the top candidate is far
// enough ahead that confirming it by name is reasonable; deciding is the
// operator's, out loud.
func MatchSessions(query string, rows []SessionRow) (candidates []Candidate, topIsClear bool) {
	tokens := normalizeTokens(query)
	if len(tokens) == 0 {
		return nil, false
	}

	scored := make([]Candidate, 0, len(rows))
	for _, row := range rows {
		if row.ID == "" {
			continue
		}
		if score := scoreRow(tokens, row); score >= matchFloor {
			scored = append(scored, Candidate{Row: row, Score: score})
		}
	}

	sort.SliceStable(scored, func(i, j int) bool {
		// Bucketed, so a hundredth of a point cannot outrank "this one is
		// waiting for you".
		if a, b := scoreBucket(scored[i].Score), scoreBucket(scored[j].Score); a != b {
			return a > b
		}
		if a, b := AttentionRank(scored[i].Row.Attention), AttentionRank(scored[j].Row.Attention); a != b {
			return a < b
		}
		return scored[i].Row.LastActivity > scored[j].Row.LastActivity
	})

	if len(scored) == 0 {
		return nil, false
	}

	topIsClear = scored[0].Score >= clearScore &&
		(len(scored) == 1 || scored[0].Score-scored[1].Score >= clearMargin)

	if len(scored) > maxCandidates {
		scored = scored[:maxCandidates]
	}
	return scored, topIsClear
}

// MatchProjects narrows a project list by what the operator called it.
//
// The same normalized matcher as [MatchSessions], for the same reason: a
// project name said out loud arrives mangled, and "web tickets" has to reach
// "webtickets". It ranks and filters; like everything else here it never picks,
// and an empty query keeps the list in the order it arrived — which is already
// most recently worked in first.
func MatchProjects(query string, rows []ProjectRow) []ProjectRow {
	tokens := normalizeTokens(query)
	if len(tokens) == 0 {
		return rows
	}

	type scoredProject struct {
		row   ProjectRow
		score float64
	}

	scored := make([]scoredProject, 0, len(rows))
	for _, row := range rows {
		if row.ID == "" {
			continue
		}
		// A project has a name and a slug and nothing else worth matching, so it
		// borrows the session scorer's project fields rather than growing a
		// second scoring rule to drift from this one.
		field := SessionRow{ProjectName: row.Name, ProjectSlug: row.Slug}
		score := scoreRow(tokens, field)
		// A slug is a compound word and a transcript splits it: "webtickets"
		// comes back as "web tickets", which scores as two weak partial matches
		// and falls under the floor. So the run-together spelling is tried too,
		// and the better of the two wins.
		if joined := strings.Join(tokens, ""); len(tokens) > 1 {
			score = max(score, scoreRow([]string{joined}, field))
		}
		if score < matchFloor {
			continue
		}
		scored = append(scored, scoredProject{row: row, score: score})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if a, b := scoreBucket(scored[i].score), scoreBucket(scored[j].score); a != b {
			return a > b
		}
		return scored[i].row.LastActivity > scored[j].row.LastActivity
	})

	out := make([]ProjectRow, 0, len(scored))
	for _, s := range scored {
		out = append(out, s.row)
	}
	return out
}

// scoreBucket coarsens a score so that near-ties are ties.
func scoreBucket(score float64) int { return int(score*20 + 0.5) }

// scoreRow is the mean best match of the query's tokens against the row, with a
// bonus for a name that matches outright.
func scoreRow(query []string, row SessionRow) float64 {
	fields := []struct {
		tokens []string
		weight float64
	}{
		{normalizeTokens(row.Name), weightName},
		{normalizeTokens(row.ProjectName), weightProject},
		{normalizeTokens(row.ProjectSlug), weightProject},
		{normalizeTokens(row.MachineName), weightMachine},
	}

	var total float64
	for _, token := range query {
		var best float64
		for _, field := range fields {
			if s := bestTokenScore(token, field.tokens) * field.weight; s > best {
				best = s
			}
		}
		total += best
	}
	score := total / float64(len(query))

	// The whole name, said correctly, is not a coincidence.
	name := strings.Join(normalizeTokens(row.Name), " ")
	if name != "" && name == strings.Join(query, " ") {
		return 1
	}
	// Every word of the query is in the name, in some form: "the reconnect one"
	// against "Reconnect Drops" should beat a project-only match.
	if coversAll(query, normalizeTokens(row.Name)) {
		score += 0.1
	}
	return min(score, 1)
}

// bestTokenScore is how well one query token matches any token of a field.
func bestTokenScore(token string, fieldTokens []string) float64 {
	var best float64
	for _, candidate := range fieldTokens {
		var score float64
		switch {
		case candidate == token:
			score = scoreExact
		case sharedPrefix(candidate, token):
			score = scorePrefix
		case len(token) >= 4 && strings.Contains(candidate, token):
			score = scoreInside
		case len(candidate) >= 4 && strings.Contains(token, candidate):
			score = scoreInside
		}
		if score > best {
			best = score
		}
	}
	return best
}

// sharedPrefix reports whether two tokens are the same word heard differently:
// "dialog" and "dialogue", "recon" and "reconnect". Four characters, because
// three-letter prefixes match half the dictionary.
func sharedPrefix(a, b string) bool {
	if len(a) < 4 || len(b) < 4 {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// coversAll reports whether every query token has a match in the field.
func coversAll(query, fieldTokens []string) bool {
	if len(fieldTokens) == 0 {
		return false
	}
	for _, token := range query {
		if bestTokenScore(token, fieldTokens) == 0 {
			return false
		}
	}
	return true
}

// filler is the words a person says around a name that carry no identity.
// Dropped so "the agentique one please" scores like "agentique".
var filler = map[string]bool{
	"the": true, "a": true, "an": true, "one": true, "session": true,
	"sessions": true, "please": true, "that": true, "this": true,
	"my": true, "on": true, "in": true, "for": true, "to": true, "of": true,
}

// normalizeTokens lowercases, drops punctuation, and removes conversational
// filler. Punctuation matters here: a transcript writes "dialog," and a session
// is called "dialog".
func normalizeTokens(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if filler[field] {
			continue
		}
		out = append(out, field)
	}
	// A query that is nothing but filler still has to mean something, so keep
	// it rather than matching everything equally.
	if len(out) == 0 {
		return fields
	}
	return out
}
