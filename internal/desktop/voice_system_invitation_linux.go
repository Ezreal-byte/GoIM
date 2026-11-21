//go:build linux

package desktop

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

func useSystemCallInvitation() bool {
	return os.Getenv("VIVA_HEADLESS") != "1" && (strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland") || os.Getenv("WAYLAND_DISPLAY") != "")
}

func waitSystemCallAction(ctx context.Context, n *systemCallInvitation) (string, error) {
	const service = "org.freedesktop.Notifications"
	const path = dbus.ObjectPath("/org/freedesktop/Notifications")
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return "", err
	}
	defer conn.Close()
	setup, finish := context.WithTimeout(ctx, 3*time.Second)
	defer finish()
	obj := conn.Object(service, path)
	var caps []string
	if err := obj.CallWithContext(setup, service+".GetCapabilities", 0).Store(&caps); err != nil {
		return "", err
	}
	if !slices.Contains(caps, "actions") {
		return "", errors.New("notification service lacks call actions")
	}
	// Pin the name that answered the capability request. An unrelated bus
	// peer or a replacement notification daemon must not answer a call.
	var owner string
	if err := conn.BusObject().CallWithContext(setup, "org.freedesktop.DBus.GetNameOwner", 0, service).Store(&owner); err != nil {
		return "", err
	}
	obj = conn.Object(owner, path)
	signals := make(chan *dbus.Signal, 8)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)
	if err := conn.AddMatchSignalContext(setup, dbus.WithMatchSender(owner), dbus.WithMatchObjectPath(path), dbus.WithMatchInterface(service)); err != nil {
		return "", err
	}
	icon := "goim"
	if filepath.IsAbs(n.avatar) {
		icon = (&url.URL{Scheme: "file", Path: n.avatar}).String()
	}
	actions := []string{"call-stop", n.decline, "call-start", n.accept}
	hints := map[string]dbus.Variant{"category": dbus.MakeVariant("call.incoming"), "urgency": dbus.MakeVariant(byte(2)), "suppress-sound": dbus.MakeVariant(true), "transient": dbus.MakeVariant(true)}
	if slices.Contains(caps, "action-icons") {
		hints["action-icons"] = dbus.MakeVariant(true)
	}
	var id uint32
	// Once sent, collect Notify's reply even if the call ends immediately so
	// its actual server ID can be withdrawn.
	if err := setup.Err(); err != nil {
		return "", err
	}
	request, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if err := obj.CallWithContext(request, service+".Notify", 0, "GoIM", uint32(0), icon, n.name, n.body, actions, hints, int32(time.Minute/time.Millisecond)).Store(&id); err != nil {
		return "", err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = obj.CallWithContext(cleanup, service+".CloseNotification", 0, id).Err
	}()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-conn.Context().Done():
			return "", errors.New("notification bus closed")
		case s, open := <-signals:
			if !open {
				return "", errors.New("notification bus closed")
			}
			if s == nil || s.Sender != owner || s.Path != path || len(s.Body) != 2 || s.Body[0] != id {
				continue
			}
			switch s.Name {
			case service + ".ActionInvoked":
				switch s.Body[1] {
				case "call-stop":
					return "reject", nil
				case "call-start":
					return "accept", nil
				}
			case service + ".NotificationClosed":
				return "", nil
			}
		}
	}
}
