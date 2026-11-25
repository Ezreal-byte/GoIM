package remotedesktop

import (
	"sync"

	"goim/internal/domain"
)

const inputQueueLimit = 64

// InputQueue buffers local viewer input for one helper writer. Consecutive
// pointer positions with the same modifiers replace each other while waiting.
// Discrete actions remain in order on either side of pointer movement.
type InputQueue struct {
	mu    sync.Mutex
	ready chan struct{}
	items [inputQueueLimit]domain.RemoteDesktopInput
	head  int
	count int
}

func NewInputQueue() *InputQueue {
	return &InputQueue{ready: make(chan struct{}, 1)}
}

// Push reports false if the queue cannot accept the event.
func (q *InputQueue) Push(input domain.RemoteDesktopInput) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if input.Action == "move" && q.count != 0 {
		last := &q.items[(q.head+q.count-1)%len(q.items)]
		if last.Action == "move" && last.Modifiers == input.Modifiers {
			*last = input
			return true
		}
	}
	if q.count == len(q.items) {
		return false
	}
	q.items[(q.head+q.count)%len(q.items)] = input
	q.count++
	if q.count == 1 {
		q.signal()
	}
	return true
}

// Ready wakes the single consumer when it can call Pop. A nil queue has no
// inputs, as on a host that only shares its screen.
func (q *InputQueue) Ready() <-chan struct{} {
	if q == nil {
		return nil
	}
	return q.ready
}

// Pop takes the oldest queued event and wakes the consumer again if work remains.
func (q *InputQueue) Pop() (domain.RemoteDesktopInput, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	select {
	case <-q.ready:
	default:
	}
	if q.count == 0 {
		return domain.RemoteDesktopInput{}, false
	}
	input := q.items[q.head]
	q.items[q.head] = domain.RemoteDesktopInput{}
	q.head = (q.head + 1) % len(q.items)
	q.count--
	if q.count != 0 {
		q.signal()
	}
	return input, true
}

func (q *InputQueue) signal() {
	select {
	case q.ready <- struct{}{}:
	default:
	}
}
