//go:build !windows && !linux && (!darwin || !cgo)

package systemauth

import "fmt"

func verify(string) error {
	return fmt.Errorf("%w on this platform", ErrUnavailable)
}

func description() string {
	return "系统验证"
}
