package desktop

import (
	"context"
	"errors"
	"fmt"
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
	checkError   error
	checkRelease <-chan struct{}
	notesStarted chan struct{}
	notesRelease <-chan struct{}
}

func (c *updateTestClient) counts() (checks, notes int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.checkCount, c.notesCount
}

func (c *updateTestClient) Check(ctx context.Context) (selfupdate.Offer, error) {
	c.mu.Lock()
	c.checkCount++
	c.mu.Unlock()
	if c.checkStarted != nil {
		select {
		case c.checkStarted <- struct{}{}:
		default:
		}
	}
	if c.checkRelease != nil {
		select {
		case <-c.checkRelease:
		case <-ctx.Done():
			return selfupdate.Offer{}, ctx.Err()
		}
	}
	return c.offer, c.checkError
}

func (c *updateTestClient) ReleaseNotes(ctx context.Context, _ selfupdate.Offer) (string, error) {
	c.mu.Lock()
	c.notesCount++
	c.mu.Unlock()
	if c.notesStarted != nil {
		close(c.notesStarted)
	}
	if c.notesRelease != nil {
		select {
		case <-c.notesRelease:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return c.notes, nil
}

func (*updateTestClient) Download(context.Context, selfupdate.Offer, func(int64, int64)) (string, error) {
	return "", nil
}

func (*updateTestClient) Install(selfupdate.Offer, string, []string, func()) error {
	return nil
}

func TestAutomaticUpdateCheckReenableChecksFromOfferedState(t *testing.T) {
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
	select {
	case <-client.checkStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("reenabling did not check again")
	}
}

func TestAutomaticUpdatePeriodicRetryDisableAndStop(t *testing.T) {
	client := &updateTestClient{checkStarted: make(chan struct{}, 8), checkError: errors.New("offline")}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	controller := &updateController{ctx: ctx, stop: cancel, client: client, automatic: true,
		view: updateView{phase: updateCurrent}, wake: func() {}, scheduleWake: make(chan struct{}, 1),
		checkInterval: time.Hour, retryInterval: 15 * time.Millisecond}
	done := make(chan struct{})
	go func() { controller.runAutomatic(); close(done) }()
	for count := 0; count < 2; count++ {
		select {
		case <-client.checkStarted:
		case <-time.After(2 * time.Second):
			t.Fatal("periodic retry did not run")
		}
	}
	controller.setAutomatic(false)
	time.Sleep(40 * time.Millisecond)
	checks, _ := client.counts()
	time.Sleep(40 * time.Millisecond)
	if next, _ := client.counts(); next != checks {
		t.Fatal("disabled updater still checked")
	}
	controller.setAutomatic(true)
	select {
	case <-client.checkStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("enable did not retry failure")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
}

func TestAutomaticUpdatePeriodNeverOverlapsOrDiscardsReadyInstaller(t *testing.T) {
	blocked := make(chan struct{})
	client := &updateTestClient{checkStarted: make(chan struct{}, 8), checkRelease: blocked}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	controller := &updateController{ctx: ctx, stop: cancel, client: client, automatic: true,
		wake: func() {}, scheduleWake: make(chan struct{}, 1), checkInterval: 15 * time.Millisecond}
	go controller.runAutomatic()
	select {
	case <-client.checkStarted:
	case <-time.After(time.Second):
		t.Fatal("first check missing")
	}
	for count := 0; count < 10; count++ {
		controller.checkNow()
		controller.reschedule()
	}
	time.Sleep(40 * time.Millisecond)
	if checks, _ := client.counts(); checks != 1 {
		t.Fatalf("overlapping checks: %d", checks)
	}
	close(blocked)
	select {
	case <-client.checkStarted:
	case <-time.After(time.Second):
		t.Fatal("successful checks did not repeat")
	}
	controller.setAutomatic(false)
	deadline := time.Now().Add(time.Second)
	for controller.snapshot().phase == updateChecking && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	controller.change(func(view *updateView) { view.phase = updateReady })
	checks, _ := client.counts()
	controller.setAutomatic(true)
	time.Sleep(50 * time.Millisecond)
	if next, _ := client.counts(); next != checks {
		t.Fatal("automatic check replaced a ready installer")
	}
}

func TestUpdateNotesFinishAfterDownloadHasStarted(t *testing.T) {
	for _, phase := range []updatePhase{updateDownloading, updateReady, updateDownloadFailed} {
		t.Run(fmt.Sprint(phase), func(t *testing.T) {
			release := make(chan struct{})
			client := &updateTestClient{
				offer: selfupdate.Offer{Version: "0.1.15", Automatic: true}, notes: "Release details",
				notesStarted: make(chan struct{}), notesRelease: release,
			}
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			controller := &updateController{ctx: ctx, client: client, wake: func() {}}
			done := make(chan struct{})
			go func() { controller.check(); close(done) }()
			select {
			case <-client.notesStarted:
			case <-time.After(time.Second):
				t.Fatal("release notes request did not start")
			}
			controller.change(func(view *updateView) { view.phase = phase })
			close(release)
			<-done
			view := controller.snapshot()
			if view.notesLoading || view.notes != client.notes || view.phase != phase {
				t.Fatalf("download transition stranded release notes: %+v", view)
			}
		})
	}
}
