package session

import (
	"context"

	claudecli "github.com/allbin/claudecli-go"
)

// CLISession abstracts a claudecli-go interactive session for testability.
// The real *claudecli.Session satisfies this interface.
type CLISession interface {
	Events() <-chan claudecli.Event
	Query(prompt string) error
	QueryWithContent(prompt string, blocks ...claudecli.ContentBlock) error
	SetPermissionMode(mode claudecli.PermissionMode) error
	SetModel(model claudecli.Model) error
	Interrupt() error
	Close() error
}

// CLIConnector creates new CLI sessions.
type CLIConnector interface {
	Connect(ctx context.Context, opts ...claudecli.Option) (CLISession, error)
}

// BlockingRunner runs a single blocking Claude CLI invocation.
type BlockingRunner interface {
	RunBlocking(ctx context.Context, prompt string, opts ...claudecli.Option) (*claudecli.BlockingResult, error)
}

// RealConnector returns a CLIConnector that wraps claudecli.New().Connect().
func RealConnector() CLIConnector { return realConnector{} }

type realConnector struct{}

func (realConnector) Connect(ctx context.Context, opts ...claudecli.Option) (CLISession, error) {
	return claudecli.New().Connect(ctx, opts...)
}

// RealBlockingRunner returns a BlockingRunner that wraps claudecli.New().RunBlocking().
func RealBlockingRunner() BlockingRunner { return realBlockingRunner{} }

type realBlockingRunner struct{}

func (realBlockingRunner) RunBlocking(ctx context.Context, prompt string, opts ...claudecli.Option) (*claudecli.BlockingResult, error) {
	return claudecli.New().RunBlocking(ctx, prompt, opts...)
}
