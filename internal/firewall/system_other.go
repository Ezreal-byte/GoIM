//go:build !linux

package firewall

import (
	"context"
	"errors"
)

var errUnsupported = errors.New("local firewall diagnostics require Linux")

func Inspect(context.Context, Config) (Report, error) { return Report{}, errUnsupported }

func Apply(context.Context, Config, Report) (Report, error) { return Report{}, errUnsupported }
