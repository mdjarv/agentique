package msggen

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	claudecli "github.com/allbin/claudecli-go"

	"github.com/allbin/agentique/backend/internal/gitops"
)

type CommitMessageResult struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type PRDescriptionResult struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func CommitMsg(ctx context.Context, sessionName, summary, diff string) (CommitMessageResult, error) {
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

	client := claudecli.New()
	result, err := client.RunBlocking(ctx, prompt,
		claudecli.WithModel(claudecli.ModelHaiku),
		claudecli.WithMaxTurns(1),
		claudecli.WithPermissionMode(claudecli.PermissionBypass),
	)
	if err != nil {
		return CommitMessageResult{}, fmt.Errorf("haiku generation failed: %w", err)
	}

	return parseCommitMessage(result.Text), nil
}

func AutoCommitMsg(ctx context.Context, sessionName, wtPath string) string {
	fallback := sessionName + ": save changes"

	diff, summary, err := gitops.UncommittedDiff(wtPath)
	if err != nil || (diff == "" && summary == "") {
		return fallback
	}

	result, err := CommitMsg(ctx, sessionName, summary, diff)
	if err != nil || result.Title == "" {
		slog.Warn("haiku commit msg failed, using fallback", "error", err)
		return fallback
	}

	if result.Description != "" {
		return result.Title + "\n\n" + result.Description
	}
	return result.Title
}

func PRDescription(ctx context.Context, sessionName, summary, diff string) (PRDescriptionResult, error) {
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

	client := claudecli.New()
	result, err := client.RunBlocking(ctx, prompt,
		claudecli.WithModel(claudecli.ModelHaiku),
		claudecli.WithMaxTurns(1),
		claudecli.WithPermissionMode(claudecli.PermissionBypass),
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
