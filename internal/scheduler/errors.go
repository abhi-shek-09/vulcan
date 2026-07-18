package scheduler
import "errors"

var (
	ErrInsufficientWorkers = errors.New("insufficient workers available")
)
