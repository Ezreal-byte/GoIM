package remotedesktop

import (
	"testing"

	"goim/internal/domain"
)

func TestInputQueueCoalescesPointerBurstsWithoutReorderingActions(t *testing.T) {
	q := NewInputQueue()
	push := func(input domain.RemoteDesktopInput) {
		t.Helper()
		if !q.Push(input) {
			t.Fatal("pointer input exhausted the queue", input)
		}
	}

	press := domain.RemoteDesktopInput{Action: "button", Down: true, Button: 1, X: 10}
	release := domain.RemoteDesktopInput{Action: "button", Button: 1, X: 1001}
	key := domain.RemoteDesktopInput{Action: "key", Key: "a", Down: true}
	push(press)
	for x := 11; x <= 1000; x++ {
		push(domain.RemoteDesktopInput{Action: "move", X: x})
	}
	withModifier := domain.RemoteDesktopInput{Action: "move", X: 1001, Modifiers: 1}
	push(withModifier)
	push(release)
	push(key)
	push(domain.RemoteDesktopInput{Action: "move", X: 1002})
	push(domain.RemoteDesktopInput{Action: "move", X: 1003})
	releaseAll := domain.RemoteDesktopInput{Action: "release"}
	push(releaseAll)

	for _, want := range []domain.RemoteDesktopInput{
		press, {Action: "move", X: 1000}, withModifier, release, key, {Action: "move", X: 1003}, releaseAll,
	} {
		select {
		case <-q.Ready():
		default:
			t.Fatal("a pending input did not wake the writer")
		}
		if got, ok := q.Pop(); !ok || got != want {
			t.Fatalf("next input = %+v, %v, want %+v", got, ok, want)
		}
	}
	select {
	case <-q.Ready():
		t.Fatal("an empty queue kept the writer awake")
	default:
	}
}

func TestInputQueueKeepsItsBoundAcrossRefills(t *testing.T) {
	q := NewInputQueue()
	for range 3 {
		for x := range inputQueueLimit {
			if !q.Push(domain.RemoteDesktopInput{Action: "button", X: x}) {
				t.Fatal("queue refused an input before reaching its limit")
			}
		}
		if q.Push(domain.RemoteDesktopInput{Action: "release"}) {
			t.Fatal("queue accepted more inputs than its limit")
		}
		for x := range inputQueueLimit {
			if input, ok := q.Pop(); !ok || input.X != x {
				t.Fatalf("queue lost input ordering after refill at %d: %+v, %v", x, input, ok)
			}
		}
	}
}
