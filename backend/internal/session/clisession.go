package session

// CLISession abstracts the claudecli-go Session interface so the session
// package can be compiled and tested on all platforms (including Windows,
// where claudecli-go does not currently build).
type CLISession interface {
	// Query sends a user message to the CLI.
	Query(prompt string) error

	// Events returns a channel of events. Closed when the turn ends.
	Events() <-chan CLIEvent

	// Wait blocks until a result or error for the current query.
	Wait() (CLIResult, error)

	// Close terminates the session.
	Close() error
}

// CLIEvent is a marker interface for events from the CLI.
type CLIEvent interface{}

// CLIResult contains the result of a completed query turn.
type CLIResult struct {
	CostUSD    float64
	DurationMS int64
	StopReason string
	Usage      any
}

// CLIConnector creates CLISession instances. Implementations wrap the
// actual claudecli-go client.
type CLIConnector interface {
	Connect(workDir string) (CLISession, error)
}
