//go:build linux

package desktop

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

type callNoticeServer struct {
	actions chan []string
	closed  chan uint32
}

func (s *callNoticeServer) GetCapabilities() ([]string, *dbus.Error) { return []string{"actions"}, nil }
func (s *callNoticeServer) Notify(app string, replace uint32, icon, title, body string, actions []string, hints map[string]dbus.Variant, expire int32) (uint32, *dbus.Error) {
	s.actions <- actions
	return 42, nil
}
func (s *callNoticeServer) CloseNotification(id uint32) *dbus.Error { s.closed <- id; return nil }

// A private session bus ensures the fixture cannot replace the user's daemon or
// deliver a synthetic incoming-call notification to their desktop.
func TestVoiceWaylandNotificationActionsAndWithdrawal(t *testing.T) {
	if os.Getenv("GOIM_TEST_CALL_BUS") != "1" {
		t.Skip("requires a private dbus-run-session")
	}
	const service = "org.freedesktop.Notifications"
	const path = dbus.ObjectPath("/org/freedesktop/Notifications")
	bus, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()
	server := &callNoticeServer{actions: make(chan []string, 1), closed: make(chan uint32, 1)}
	if err := bus.Export(server, path, service); err != nil {
		t.Fatal(err)
	}
	reply, err := bus.RequestName(service, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("cannot own fixture name: %v %v", reply, err)
	}
	for _, action := range []string{"accept", "reject", "cancel"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			type response struct {
				action string
				err    error
			}
			result := make(chan response, 1)
			go func() {
				key, err := waitSystemCallAction(ctx, &systemCallInvitation{callID: "private-test", name: "GoIM", accept: "Answer", decline: "Decline", body: "Incoming call"})
				result <- response{key, err}
			}()
			select {
			case btn := <-server.actions:
				if !slices.Equal(btn, []string{"accept", "Answer", "reject", "Decline"}) {
					t.Fatalf("actions: %q", btn)
				}
			case res := <-result:
				t.Fatalf("setup: %v", res.err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if action == "cancel" {
				cancel()
			} else {
				if err := bus.Emit(path, service+".ActionInvoked", uint32(7), "reject"); err != nil {
					t.Fatal(err)
				}
				if err := bus.Emit(path, service+".ActionInvoked", uint32(42), action); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case actual := <-result:
				if action == "cancel" {
					if actual.err != context.Canceled || actual.action != "" {
						t.Fatalf("cancellation: %+v", actual)
					}
				} else if actual.err != nil || actual.action != action {
					t.Fatalf("response: %+v", actual)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("notification action did not finish")
			}
			select {
			case id := <-server.closed:
				if id != 42 {
					t.Fatalf("closed notification %d", id)
				}
			default:
				t.Fatal("notification outlived call")
			}
		})
	}
}
