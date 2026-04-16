package ws

import (
	"errors"
	"fmt"
)

// --- Project payloads ---

type ProjectSubscribePayload struct {
	ProjectID string `json:"projectId"`
}

type ProjectGitStatusPayload struct {
	ProjectID string `json:"projectId"`
}

type ProjectFetchPayload struct {
	ProjectID string `json:"projectId"`
}

type ProjectPushPayload struct {
	ProjectID string `json:"projectId"`
}

type ProjectCommitPayload struct {
	ProjectID string `json:"projectId"`
	Message   string `json:"message"`
}

type ProjectListBranchesPayload struct {
	ProjectID string `json:"projectId"`
}

type ProjectCheckoutPayload struct {
	ProjectID string `json:"projectId"`
	Branch    string `json:"branch"`
}

type ProjectPullPayload struct {
	ProjectID string `json:"projectId"`
}

type ProjectTrackedFilesPayload struct {
	ProjectID string `json:"projectId"`
}

type ProjectCommandsPayload struct {
	ProjectID string `json:"projectId"`
}

type ProjectUncommittedFilesPayload struct {
	ProjectID string `json:"projectId"`
}

type ProjectDiscardPayload struct {
	ProjectID string `json:"projectId"`
}

type ProjectGenerateCommitMsgPayload struct {
	ProjectID string `json:"projectId"`
}

type ProjectReorderPayload struct {
	ProjectIDs []string `json:"projectIds"`
}

type ProjectSetFavoritePayload struct {
	ProjectID string `json:"projectId"`
	Favorite  bool   `json:"favorite"`
}

type ProjectActivityPayload struct {
	ProjectID string `json:"projectId"`
}

// --- Project validation errors ---

var (
	errProjectIDAndBranchRequired = errors.New("projectId and branch are required")
	errProjectIDsRequired         = errors.New("projectIds is required")
	errProjectIDAndMsgRequired    = errors.New("projectId and message are required")
)

// --- Project Validate methods ---

func (p *ProjectSubscribePayload) Validate() error {
	return validateProjectID(p.ProjectID)
}

func (p *ProjectGitStatusPayload) Validate() error {
	return validateProjectID(p.ProjectID)
}

func (p *ProjectFetchPayload) Validate() error {
	return validateProjectID(p.ProjectID)
}

func (p *ProjectPushPayload) Validate() error {
	return validateProjectID(p.ProjectID)
}

func (p *ProjectListBranchesPayload) Validate() error {
	return validateProjectID(p.ProjectID)
}

func (p *ProjectCheckoutPayload) Validate() error {
	if p.Branch == "" {
		return errProjectIDAndBranchRequired
	}
	if err := validateProjectID(p.ProjectID); err != nil {
		return err
	}
	return validateBranchName(p.Branch)
}

func (p *ProjectPullPayload) Validate() error {
	return validateProjectID(p.ProjectID)
}

func (p *ProjectCommitPayload) Validate() error {
	if p.Message == "" {
		return errProjectIDAndMsgRequired
	}
	if err := validateProjectID(p.ProjectID); err != nil {
		return err
	}
	return validateMaxLen("message", p.Message, maxCommitMsgLen)
}

func (p *ProjectTrackedFilesPayload) Validate() error {
	return validateProjectID(p.ProjectID)
}

func (p *ProjectCommandsPayload) Validate() error {
	return validateProjectID(p.ProjectID)
}

func (p *ProjectReorderPayload) Validate() error {
	if len(p.ProjectIDs) == 0 {
		return errProjectIDsRequired
	}
	if len(p.ProjectIDs) > maxBulkDeleteIDs {
		return fmt.Errorf("too many projectIds (max %d)", maxBulkDeleteIDs)
	}
	for _, id := range p.ProjectIDs {
		if err := validateUUID("projectIds[]", id); err != nil {
			return err
		}
	}
	return nil
}

func (p *ProjectSetFavoritePayload) Validate() error {
	return validateProjectID(p.ProjectID)
}

func (p *ProjectUncommittedFilesPayload) Validate() error {
	return validateProjectID(p.ProjectID)
}

func (p *ProjectDiscardPayload) Validate() error {
	return validateProjectID(p.ProjectID)
}

func (p *ProjectGenerateCommitMsgPayload) Validate() error {
	return validateProjectID(p.ProjectID)
}

func (p *ProjectActivityPayload) Validate() error {
	return validateProjectID(p.ProjectID)
}
