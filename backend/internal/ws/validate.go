package ws

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Validation limits.
const (
	maxPromptLen     = 1 << 20 // 1 MB
	maxNameLen       = 500
	maxAttachments   = 50
	maxContentLen    = 1 << 20 // 1 MB for message/content fields
	maxCommitMsgLen  = 10_000
	maxBulkDeleteIDs = 200
)

func validateUUID(field, value string) error {
	if _, err := uuid.Parse(value); err != nil {
		return fmt.Errorf("%s: invalid UUID format", field)
	}
	return nil
}

func validateOptionalUUID(field, value string) error {
	if value == "" {
		return nil
	}
	return validateUUID(field, value)
}

func validateMaxLen(field, value string, max int) error {
	if len(value) > max {
		return fmt.Errorf("%s: exceeds maximum length (%d bytes)", field, max)
	}
	return nil
}

func validateBranchName(value string) error {
	if value == "" {
		return nil
	}
	if strings.HasPrefix(value, "-") {
		return fmt.Errorf("branch: must not start with '-'")
	}
	return nil
}

func trimSpace(s string) string {
	return strings.TrimSpace(s)
}
