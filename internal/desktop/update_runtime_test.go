package desktop

import (
	"context"
	"sync"
	"testing"
	"time"

	"goim/internal/selfupdate"
)

type updateTestClient struct {
	mu           sync.Mutex
	checkCount   int
	notesCount   int
	offer        selfupdate.Offer
	notes        string
	checkStarted chan struct{}
}

func (c *updateTestClient) counts() (checks, notes int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.checkCount, c.notesCount
}

func (c *updateTestClient) Check(context.Context) (selfupdate.Offer, error) {
	c.mu.Lock()
	c.checkCount++
	c.mu.Unlock()
	if c.checkStarted != nil {
		select {
		case c.checkStarted <- struct{}{}:
		default:
		}
	}
	return c.offer, nil
}

func (c *updateTestClient) ReleaseNotes(context.Context, selfupdate.Offer) (string, error) {
	c.mu.Lock()
	c.notesCount++
	c.mu.Unlock()
	return c.notes, nil
}

func (*updateTestClient) Download(context.Context, selfupdate.Offer, func(int64, int64)) (string, error) {
	return "", nil
}

func (*updateTestClient) Install(selfupdate.Offer, string, []string, func()) error {
	return nil
}

func TestAutomaticUpdateCheckToggleTriggersOneIdleCheck(t *testing.T) {
	client := &updateTestClient{
		offer:        selfupdate.Offer{Version: "0.1.6"},
		notes:        "- Fixed detached chat windows\n- Added update controls",
		checkStarted: make(chan struct{}, 2),
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	controller := &updateController{
		ctx:       ctx,
		stop:      cancel,
		client:    client,
		automatic: false,
		view:      updateView{phase: updateIdle},
		wake:      func() {},
	}

	controller.setAutomatic(false)
	if count := controller.snapshot().phase; count != updateIdle {
		t.Fatalf("manual-only controller phase = %v", count)
	}
	controller.setAutomatic(true)
	select {
	case <-client.checkStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("enabling automatic checks did not start a check")
	}
	deadline := time.Now().Add(2 * time.Second)
	for controller.snapshot().notes == "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	view := controller.snapshot()
	if view.phase != updateOffered || view.notes != client.notes || view.notesProblem != "" {
		t.Fatalf("update view = %+v", view)
	}
	controller.setAutomatic(false)
	controller.setAutomatic(true)
	if checks, notes := client.counts(); checks != 1 || notes != 1 {
		t.Fatalf("automatic toggle reran a completed check: checks=%d notes=%d", checks, notes)
	}
}
