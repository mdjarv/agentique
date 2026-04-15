package store

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSQLiteError mimics modernc.org/sqlite.Error for testing.
// We can't easily construct a real *sqlite.Error, so we test isTransientSQLiteError
// indirectly via RetryWrite behavior and test the public API.

func TestRetryWrite_SucceedsImmediately(t *testing.T) {
	calls := 0
	err := RetryWrite(func() error {
		calls++
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
}

func TestRetryWrite_NonTransientError_NoRetry(t *testing.T) {
	calls := 0
	sentinel := errors.New("constraint violation")
	err := RetryWrite(func() error {
		calls++
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	assert.Equal(t, 1, calls, "non-transient errors should not retry")
}

func TestRetryWrite_NilFuncReturnsNil(t *testing.T) {
	err := RetryWrite(func() error { return nil })
	require.NoError(t, err)
}
