package execution

import "time"

type ExecutionConfig struct {
	TargetURL   string
	Method      string
	Headers     map[string]string
	Body        []byte
	Duration    time.Duration
	RPS         int
	Concurrency int
}
