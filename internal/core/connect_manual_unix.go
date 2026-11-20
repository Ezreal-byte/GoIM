//go:build !windows

package core

func platformConnectionProblem(error) string { return "" }
