package msggen

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	claudecli "github.com/allbin/claudecli-go"

	"github.com/allbin/agentique/backend/internal/gitops"
)

const maxRetries = 2 // 3 total attempts

// Runner runs a single blocking Claude CLI invocation.
type Runner interface {
	RunBlocking(ctx context.Context, prompt string, opts ...claudecli.Option) (*claudecli.BlockingResult, error)
}

type CommitMessageResult struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type PRDescriptionResult struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// RunWithRetry wraps RunBlocking with retry on retriable errors (rate limit, overloaded).
// Non-retriable errors fail immediately.
func RunWithRetry(ctx context.Context, runner Runner, prompt string, opts ...claudecli.Option) (*claudecli.BlockingResult, error) {
	var lastErr error
	for attempt := range maxRetries + 1 {
		result, err := runner.RunBlocking(ctx, prompt, opts...)
		if err == nil {
			return result, nil
		}
		lastErr = err

		delay := retryDelay(err, attempt)
		if delay == 0 {
			return nil, err
		}
		slog.Warn("retriable error, backing off", "attempt", attempt+1, "delay", delay, "error", err)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, lastErr
}

// retryDelay returns the backoff duration for retriable errors, or 0 for non-retriable.
func retryDelay(err error, attempt int) time.Duration {
	if errors.Is(err, claudecli.ErrRateLimit) {
		var rlErr *claudecli.RateLimitError
		if errors.As(err, &rlErr) && rlErr.RetryAfter > 0 {
			return rlErr.RetryAfter
		}
		return 30 * time.Second
	}
	if errors.Is(err, claudecli.ErrOverloaded) {
		return time.Duration(5<<attempt) * time.Second // 5s, 10s
	}
	return 0
}

func CommitMsg(ctx context.Context, runner Runner, sessionName, summary, diff string) (CommitMessageResult, error) {
	diffText := diff
	const maxDiffChars = 8000
	if len(diffText) > maxDiffChars {
		diffText = diffText[:maxDiffChars] + "\n... (truncated)"
	}

	prompt := fmt.Sprintf(
		"Generate a git commit message for these changes.\n"+
			"Session name: %s\n\n"+
			"Diff summary:\n%s\n\n"+
			"Full diff:\n%s\n\n"+
			"Respond in EXACTLY this format with no other text:\n"+
			"TITLE: <imperative mood, max 72 chars, no period>\n"+
			"DESCRIPTION:\n<optional longer explanation, 1-4 lines, explain why not what>",
		sessionName, summary, diffText,
	)

	result, err := RunWithRetry(ctx, runner, prompt,
		claudecli.WithModel(claudecli.ModelHaiku),
		claudecli.WithMaxTurns(1),
		claudecli.WithBuiltinTools(""),
		claudecli.WithSkipVersionCheck(),
		claudecli.WithStrictMCPConfig(),
		claudecli.WithDisableSlashCommands(),
		claudecli.WithSettingSources(""),
	)
	if err != nil {
		return CommitMessageResult{}, fmt.Errorf("haiku generation failed: %w", err)
	}

	return parseCommitMessage(result.Text), nil
}

func AutoCommitMsg(ctx context.Context, runner Runner, sessionName, wtPath string) string {
	fallback := sessionName + ": save changes"

	diff, summary, err := gitops.UncommittedDiff(wtPath)
	if err != nil || (diff == "" && summary == "") {
		return fallback
	}

	result, err := CommitMsg(ctx, runner, sessionName, summary, diff)
	if err != nil || result.Title == "" {
		slog.Warn("haiku commit msg failed, using fallback", "error", err)
		return fallback
	}

	if result.Description != "" {
		return result.Title + "\n\n" + result.Description
	}
	return result.Title
}

func PRDescription(ctx context.Context, runner Runner, sessionName, summary, diff string) (PRDescriptionResult, error) {
	diffText := diff
	const maxDiffChars = 8000
	if len(diffText) > maxDiffChars {
		diffText = diffText[:maxDiffChars] + "\n... (truncated)"
	}

	prompt := fmt.Sprintf(
		"Generate a GitHub pull request title and description for these changes.\n"+
			"Session name: %s\n\n"+
			"Diff summary:\n%s\n\n"+
			"Full diff:\n%s\n\n"+
			"Respond in EXACTLY this format with no other text:\n"+
			"TITLE: <short PR title, max 70 chars>\n"+
			"BODY:\n<markdown description: what changed and why, use bullet points, 2-8 lines>",
		sessionName, summary, diffText,
	)

	result, err := RunWithRetry(ctx, runner, prompt,
		claudecli.WithModel(claudecli.ModelHaiku),
		claudecli.WithMaxTurns(1),
		claudecli.WithBuiltinTools(""),
		claudecli.WithSkipVersionCheck(),
		claudecli.WithStrictMCPConfig(),
		claudecli.WithDisableSlashCommands(),
		claudecli.WithSettingSources(""),
	)
	if err != nil {
		return PRDescriptionResult{}, fmt.Errorf("haiku generation failed: %w", err)
	}

	return parsePRDescription(result.Text), nil
}

func parseCommitMessage(text string) CommitMessageResult {
	text = strings.TrimSpace(text)

	titleIdx := strings.Index(text, "TITLE:")
	descIdx := strings.Index(text, "DESCRIPTION:")

	var title, desc string
	if titleIdx >= 0 && descIdx > titleIdx {
		title = strings.TrimSpace(text[titleIdx+len("TITLE:") : descIdx])
		desc = strings.TrimSpace(text[descIdx+len("DESCRIPTION:"):])
	} else if titleIdx >= 0 {
		title = strings.TrimSpace(text[titleIdx+len("TITLE:"):])
	} else {
		lines := strings.SplitN(text, "\n", 2)
		title = strings.TrimSpace(lines[0])
		if len(lines) > 1 {
			desc = strings.TrimSpace(lines[1])
		}
	}

	if len(title) > 72 {
		title = title[:72]
	}

	return CommitMessageResult{Title: title, Description: desc}
}

func parsePRDescription(text string) PRDescriptionResult {
	text = strings.TrimSpace(text)

	titleIdx := strings.Index(text, "TITLE:")
	bodyIdx := strings.Index(text, "BODY:")

	var title, body string
	if titleIdx >= 0 && bodyIdx > titleIdx {
		title = strings.TrimSpace(text[titleIdx+len("TITLE:") : bodyIdx])
		body = strings.TrimSpace(text[bodyIdx+len("BODY:"):])
	} else if titleIdx >= 0 {
		title = strings.TrimSpace(text[titleIdx+len("TITLE:"):])
	} else {
		lines := strings.SplitN(text, "\n", 2)
		title = strings.TrimSpace(lines[0])
		if len(lines) > 1 {
			body = strings.TrimSpace(lines[1])
		}
	}

	if len(title) > 70 {
		title = title[:70]
	}

	return PRDescriptionResult{Title: title, Body: body}
}
