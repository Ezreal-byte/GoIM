// Package systemauth asks the operating system to authenticate the current
// user. It never reads, stores or passes system credentials to GoIM.
package systemauth

import "errors"

var (
	ErrCanceled         = errors.New("system verification was canceled")
	ErrUnavailable      = errors.New("system verification is unavailable")
	ErrNotConfigured    = errors.New("system verification is not configured")
	ErrDisabledByPolicy = errors.New("system verification is disabled by policy")
	ErrBusy             = errors.New("the system verification device is busy")
	ErrRetriesExhausted = errors.New("too many system verification attempts")
	ErrFailed           = errors.New("system verification failed")
)

// Verify presents the platform's own authentication UI for reason. A nil
// result means the operating system confirmed the current user.
func Verify(reason string) error {
	return verify(reason)
}

// Description names the verification methods the current platform offers.
func Description() string {
	return description()
}
