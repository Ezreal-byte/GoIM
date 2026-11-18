package systemauth

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
)

const (
	policyAction = "dev.goim.desktop.unlock"
	// POLKIT_CHECK_AUTHORIZATION_FLAGS_ALLOW_USER_INTERACTION
	policyAllowInteraction = uint32(1)
)

func description() string {
	return "系统认证（密码或指纹）"
}

func verify(reason string) error {
	startTime, err := processStartTime()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	conn, err := dbus.SystemBus()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	subject := []any{
		"unix-process",
		map[string]dbus.Variant{
			"pid":        dbus.MakeVariant(uint32(os.Getpid())),
			"start-time": dbus.MakeVariant(startTime),
		},
	}
	details := map[string]string{}
	if strings.TrimSpace(reason) != "" {
		details["reason"] = reason
	}

	var authorized, challenged bool
	var results map[string]string
	call := conn.Object("org.freedesktop.PolicyKit1", "/org/freedesktop/PolicyKit1/Authority").
		Call("org.freedesktop.PolicyKit1.Authority.CheckAuthorization", 0,
			subject, policyAction, details, policyAllowInteraction, "")
	if call.Err != nil {
		var dbusErr dbus.Error
		if errors.As(call.Err, &dbusErr) {
			switch dbusErr.Name {
			case "org.freedesktop.PolicyKit1.Error.UnknownAction":
				return fmt.Errorf("%w: install the GoIM polkit policy", ErrNotConfigured)
			case "org.freedesktop.PolicyKit1.Error.Cancelled":
				return ErrCanceled
			case "org.freedesktop.PolicyKit1.Error.NotAuthorized":
				return ErrFailed
			}
		}
		return fmt.Errorf("%w: %v", ErrUnavailable, call.Err)
	}
	if err := call.Store(&authorized, &challenged, &results); err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if authorized {
		return nil
	}
	if challenged {
		return ErrCanceled
	}
	return ErrFailed
}

// processStartTime is field 22 of /proc/self/stat. Polkit compares it with
// the PID so a reused process ID cannot borrow another process's identity.
func processStartTime() (uint64, error) {
	raw, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, err
	}
	end := bytes.LastIndexByte(raw, ')')
	if end < 0 {
		return 0, errors.New("invalid /proc/self/stat")
	}
	fields := bytes.Fields(raw[end+1:])
	if len(fields) < 20 {
		return 0, errors.New("incomplete /proc/self/stat")
	}
	start, err := strconv.ParseUint(string(fields[19]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid process start time: %w", err)
	}
	return start, nil
}
