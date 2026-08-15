package execution

import "context"

type Executor interface {
	Execute(ctx context.Context, cfg ExecutionConfig) Result
}
