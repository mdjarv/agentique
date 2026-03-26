package main

// projectBrief is the subset of project fields used by CLI commands.
type projectBrief struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// sessionBrief is the subset of session fields used by CLI commands.
type sessionBrief struct {
	ID             string `json:"id"`
	ProjectID      string `json:"projectId"`
	Name           string `json:"name"`
	State          string `json:"state"`
	Connected      bool   `json:"connected"`
	Model          string `json:"model"`
	WorktreeBranch string `json:"worktreeBranch"`
}
