//go:build !linux

package desktop

import "context"

func useSystemCallInvitation() bool                                               { return false }
func waitSystemCallAction(context.Context, *systemCallInvitation) (string, error) { return "", nil }
