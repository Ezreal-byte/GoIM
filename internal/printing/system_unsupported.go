//go:build !linux && !darwin && !windows

package printing

import (
	"context"
	"errors"

	"goim/internal/domain"
)

func (System) List(context.Context) ([]Printer, error) {
	return nil, errors.New("当前系统暂不支持打印队列")
}

func (System) Submit(context.Context, string, string, domain.PrintOptions) (Receipt, error) {
	return Receipt{}, errors.New("当前系统暂不支持打印队列")
}

func (System) Status(context.Context, Receipt) (State, error) {
	return StateUnknown, nil
}
