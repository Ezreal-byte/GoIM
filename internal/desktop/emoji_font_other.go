//go:build !linux

package desktop

func registerEmojiFallback() func() { return func() {} }
