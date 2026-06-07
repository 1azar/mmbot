package mmbot

import "errors"

var (
	// ErrAlreadyRun indicates that Run was called more than once.
	ErrAlreadyRun = errors.New("mmbot: bot has already been run")
	// ErrRoutesFrozen indicates that route configuration changed after Run.
	ErrRoutesFrozen = errors.New("mmbot: routes cannot be changed after Run")
	// ErrHandlerQueueFull indicates that a drop-newest queue rejected a message.
	ErrHandlerQueueFull = errors.New("mmbot: handler queue is full")
	// ErrUnauthorized is returned by guards when access is denied.
	ErrUnauthorized = errors.New("mmbot: unauthorized")
	// ErrShutdownTimeout indicates that handlers exceeded ShutdownTimeout.
	ErrShutdownTimeout = errors.New("mmbot: shutdown timeout")
	// ErrInvalidCommandSyntax is wrapped by CommandParseError.
	ErrInvalidCommandSyntax = errors.New("mmbot: invalid command syntax")
)

// CommandParseError describes malformed quoting or escaping in command input.
type CommandParseError struct {
	Offset int
	Reason string
}

func (e *CommandParseError) Error() string {
	return "mmbot: invalid command syntax at byte " + itoa(e.Offset) + ": " + e.Reason
}

func (e *CommandParseError) Unwrap() error { return ErrInvalidCommandSyntax }

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}
