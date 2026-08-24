package main

// projectBrief is the subset of project fields used by CLI commands.
type projectBrief struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Path string `json:"path"`
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
	WorktreeMerged bool   `json:"worktreeMerged"`
	CommitsAhead   int    `json:"commitsAhead"`
	CommitsBehind  int    `json:"commitsBehind"`
	TurnCount      int    `json:"turnCount"`
	ArchivedAt     string `json:"archivedAt"`
}

// archived reports whether the user filed this session away — the same bit the
// sidebar's Archived section reads.
func (s sessionBrief) archived() bool { return s.ArchivedAt != "" }
