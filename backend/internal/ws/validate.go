package ws

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Validation limits.
const (
	maxPromptLen = 1 << 20 // 1 MB
	maxNameLen   = 500
	// Attachment limits. The binding constraint is not per file: attachments ride
	// to the provider as inline base64, and Anthropic caps the whole *request* at
	// 32 MB, replayed with the conversation history on every turn. So the product
	// is what has to fit — 3 x 7 MiB is ~28 MB encoded, where 4 would be 37 MB and
	// the API would reject every turn from then on. 7 MiB also stays under
	// Anthropic's 10 MB-base64 per-image cap. Move the count and the size
	// together, and keep the data-URL cap above ceil(bytes * 4/3) plus the
	// `data:<mime>;base64,` prefix or the cheap length check rejects a legal file.
	maxAttachments          = 3
	maxAttachmentBytes      = 7 << 20
	maxAttachmentDataURLLen = 10 << 20
	maxContentLen           = 1 << 20 // 1 MB for message/content fields
	maxCommitMsgLen         = 10_000
	maxBulkDeleteIDs        = 200
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
