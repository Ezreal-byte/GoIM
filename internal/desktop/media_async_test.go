package desktop

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

func waitForMediaWorker(t *testing.T, worker *mediaWorker) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !worker.settled() {
		if time.Now().After(deadline) {
			t.Fatal("media worker did not settle")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestMediaWorkerCloseDoesNotWaitForActiveJob(t *testing.T) {
	worker := newMediaWorker(nil)
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	worker.setBeforeJob(func(job mediaJob) {
		if job.kind != mediaJobImage {
			return
		}
		startedOnce.Do(func() {
			close(started)
			<-release
		})
	})
	worker.submitOne(mediaJob{
		key:  mediaJobKey{kind: mediaJobImage, name: "blocked.png"},
		kind: mediaJobImage, path: "blocked.png", generation: 1, imageLimit: 512,
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("media worker did not start the blocking image job")
	}

	closed := make(chan struct{})
	go func() {
		worker.close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("closing the media worker waited for active filesystem work")
	}
	close(release)
	select {
	case <-worker.done:
	case <-time.After(2 * time.Second):
		t.Fatal("media worker did not stop after its active job ended")
	}
}

func TestUICloseCancelsBlockedMediaWorker(t *testing.T) {
	shell := New(nil)
	started := make(chan struct{})
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var startedOnce sync.Once
	shell.media.setBeforeJob(func(job mediaJob) {
		if job.kind != mediaJobImage {
			return
		}
		startedOnce.Do(func() {
			close(started)
			<-release
		})
	})
	shell.media.submitOne(mediaJob{
		key:  mediaJobKey{kind: mediaJobImage, name: "close.png"},
		kind: mediaJobImage, path: "close.png", generation: 1, imageLimit: 512,
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("media worker did not start the blocking image job")
	}
	closed := make(chan struct{})
	go func() {
		shell.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("UI.Close waited for queued or active media work")
	}
	close(release)
	select {
	case <-shell.media.done:
	case <-time.After(2 * time.Second):
		t.Fatal("UI.Close did not stop the media worker")
	}
}

func TestMediaWorkerCoalescesQueuedImageJobs(t *testing.T) {
	worker := newMediaWorker(nil)
	defer worker.close()
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	worker.setBeforeJob(func(job mediaJob) {
		if job.kind != mediaJobImage {
			return
		}
		startedOnce.Do(func() {
			close(started)
			<-release
		})
	})
	key := mediaJobKey{kind: mediaJobImage, name: "coalesced.png"}
	worker.submitOne(mediaJob{
		key: key, kind: mediaJobImage, path: "coalesced.png", generation: 1, imageLimit: 512,
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("media worker did not start")
	}
	for generation := uint64(2); generation <= 50; generation++ {
		worker.submitOne(mediaJob{
			key: key, kind: mediaJobImage, path: "coalesced.png",
			generation: generation, imageLimit: 512,
		})
	}
	close(release)
	waitForMediaWorker(t, worker)
	batch, ok := worker.take()
	if !ok || len(batch.images) != 1 {
		t.Fatalf("coalesced results = %+v, present=%v", batch, ok)
	}
	if batch.images[0].generation != 50 {
		t.Fatalf("coalesced image generation = %d, want 50", batch.images[0].generation)
	}
}

func TestBlockedImageDecodeDoesNotBlockOnWake(t *testing.T) {
	harness := newShellTestHarness(t, shellTestState(t))
	avatar := writeExtraTestPNG(t, "async-avatar.png")
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })

	var startedOnce sync.Once
	harness.ui.media.setBeforeJob(func(job mediaJob) {
		if job.kind != mediaJobImage {
			return
		}
		startedOnce.Do(func() {
			close(started)
			<-release
		})
	})

	state := shellTestState(t)
	state.Self.Avatar = avatar
	state.Revision++
	harness.backend.setState(state)
	select {
	case <-harness.driver.Wakes():
	case <-time.After(2 * time.Second):
		t.Fatal("backend update did not wake the shell")
	}

	pumped := make(chan error, 1)
	go func() {
		pumped <- harness.driver.Pump()
	}()
	select {
	case err := <-pumped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("window wake waited for image decode")
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("image decode was not handed to the media worker")
	}
	releaseOnce.Do(func() { close(release) })
	harness.waitFor(func() bool {
		return harness.ui.avatarBitmap(avatar).Width > 1
	})
}

func TestInitialAvatarAppearsAfterBindingWithoutBackendUpdate(t *testing.T) {
	avatar := writeExtraTestPNG(t, "initial-avatar.png")
	state := shellTestState(t)
	state.Self.Avatar = avatar
	harness := newShellTestHarness(t, state)
	harness.waitFor(func() bool {
		return harness.ui.avatarBitmap(avatar).Width > 1
	})
}

func TestFileAvailabilityUpdatesOnWakeAndDetectsDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "available.txt")
	if err := os.WriteFile(path, []byte("available"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := shellTestState(t)
	state.Files[0].SourcePath = path
	harness := newShellTestHarness(t, state)
	harness.wake()
	harness.waitFor(func() bool {
		return harness.ui.fileAvailable["file-1"]
	})
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	harness.wake()
	harness.waitFor(func() bool {
		return !harness.ui.fileAvailable["file-1"]
	})
}

func TestBlockedFileAvailabilityProbeDoesNotBlockOnWake(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slow-stat.txt")
	if err := os.WriteFile(path, []byte("slow"), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := newShellTestHarness(t, shellTestState(t))
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var startedOnce sync.Once
	harness.ui.media.setBeforeJob(func(job mediaJob) {
		if job.kind != mediaJobFileAvailability {
			return
		}
		startedOnce.Do(func() {
			close(started)
			<-release
		})
	})

	state := shellTestState(t)
	state.Files[0].SourcePath = path
	state.Revision++
	harness.backend.setState(state)
	select {
	case <-harness.driver.Wakes():
	case <-time.After(2 * time.Second):
		t.Fatal("backend update did not wake the shell")
	}
	pumped := make(chan error, 1)
	go func() {
		pumped <- harness.driver.Pump()
	}()
	select {
	case err := <-pumped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("window wake waited for file availability")
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("file availability was not handed to the media worker")
	}
	releaseOnce.Do(func() { close(release) })
	harness.waitFor(func() bool {
		return harness.ui.fileAvailable["file-1"]
	})
}

func TestLocalOpenRunsOnMediaWorkerBeforeWake(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker-open.txt")
	if err := os.WriteFile(path, []byte("open"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := panelTestDefaultState(t)
	state.Files = []domain.File{{
		ID: "shared-open", Name: "worker-open.txt", Size: 4,
		SenderID: "self", SourcePath: path, Shared: true, CreatedAt: time.Now(),
	}}
	harness := panelTestNewHarness(t, panelModuleShares, state)

	started := make(chan struct{})
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var startedOnce sync.Once
	harness.media.setBeforeJob(func(job mediaJob) {
		if job.kind != mediaJobLocalAction {
			return
		}
		startedOnce.Do(func() {
			close(started)
			<-release
		})
	})

	harness.click(harness.panels.ids.shareOpen.At(0))
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("local open was not handed to the media worker")
	}
	if opened, ok := harness.driver.LastOpenedPath(); ok {
		t.Fatalf("open ran on the window thread before the media worker: %q", opened)
	}
	close(release)
	waitForMediaWorker(t, harness.media)
	if opened, ok := harness.driver.LastOpenedPath(); !ok || opened != path {
		t.Fatalf("worker opened %q, observed %v; want %q", opened, ok, path)
	}
}

func TestMediaBatchRejectsStaleResults(t *testing.T) {
	shell := New(nil)
	defer shell.Close()
	oldPath := filepath.Join(t.TempDir(), "old.txt")
	newPath := filepath.Join(t.TempDir(), "new.txt")
	shell.fileLocalPath["file-1"] = newPath
	shell.fileAvailable["file-1"] = false
	shell.mediaFiles["file-1"] = mediaFileRequest{path: newPath, generation: 2}

	shell.applyMediaBatch(mediaResultBatch{files: []mediaFileResult{{
		fileID: "file-1", path: oldPath, generation: 1, available: true,
	}}})
	if shell.fileAvailable["file-1"] {
		t.Fatal("a stale path result replaced current file availability")
	}
	if shell.mediaFiles["file-1"].generation != 2 {
		t.Fatal("a stale file result consumed the current request")
	}

	shell.applyMediaBatch(mediaResultBatch{files: []mediaFileResult{{
		fileID: "file-1", path: newPath, generation: 2, available: true,
	}}})
	if !shell.fileAvailable["file-1"] {
		t.Fatal("the current file availability result was not applied")
	}
	if _, ok := shell.mediaFiles["file-1"]; ok {
		t.Fatal("the applied file request stayed queued")
	}

	shell.mediaImageQueued[oldPath] = 4
	shell.mediaDesiredImages[oldPath] = false
	shell.applyMediaBatch(mediaResultBatch{images: []mediaImageResult{{
		path: oldPath, generation: 4, decoded: true,
		bitmap: ui.Bitmap{Pix: []byte{1, 2, 3, 4}, Width: 1, Height: 1, Stride: 4},
	}}})
	if _, ok := shell.imageCache[oldPath]; ok {
		t.Fatal("an image result was applied after its snapshot stopped wanting it")
	}
	if _, ok := shell.mediaImageQueued[oldPath]; ok {
		t.Fatal("the rejected image request stayed queued")
	}

	cached := ui.Bitmap{Pix: []byte{4, 3, 2, 1}, Width: 1, Height: 1, Stride: 4, ID: 99, Version: 1}
	shell.imageCache[newPath] = cached
	shell.mediaDesiredImages[newPath] = true
	shell.mediaImageQueued[newPath] = 6
	shell.applyMediaBatch(mediaResultBatch{images: []mediaImageResult{{
		path: newPath, generation: 5, decoded: true,
		bitmap: ui.Bitmap{Pix: []byte{9, 9, 9, 9}, Width: 1, Height: 1, Stride: 4},
	}}})
	if got := shell.imageCache[newPath]; got.ID != cached.ID || got.Pix[0] != cached.Pix[0] {
		t.Fatal("an older image generation replaced the current cached bitmap")
	}
	if shell.mediaImageQueued[newPath] != 6 {
		t.Fatal("an older image generation consumed the current request")
	}
}

func TestImageStampChangeReplacesBitmapAndUnchangedResultKeepsVersion(t *testing.T) {
	avatar := writeExtraTestPNG(t, "stamp.png")
	state := shellTestState(t)
	state.Self.Avatar = avatar
	harness := newShellTestHarness(t, state)
	harness.wake()
	harness.waitFor(func() bool {
		return harness.ui.avatarBitmap(avatar).Width > 1
	})
	first := harness.ui.avatarBitmap(avatar)
	if first.ID == 0 || first.Version != 1 {
		t.Fatalf("first bitmap identity = id %d version %d", first.ID, first.Version)
	}

	source := image.NewNRGBA(image.Rect(0, 0, 3, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: 20, G: 120, B: 240, A: 255})
		}
	}
	file, err := os.Create(avatar)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, source); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	modified := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(avatar, modified, modified); err != nil {
		t.Fatal(err)
	}
	harness.wake()
	harness.waitFor(func() bool {
		bitmap := harness.ui.avatarBitmap(avatar)
		return bitmap.Width == 3 && bitmap.Height == 3 && bitmap.ID != first.ID
	})
	second := harness.ui.avatarBitmap(avatar)
	if second.Version != 1 {
		t.Fatalf("redecoded bitmap version = %d, want 1", second.Version)
	}

	shell := New(nil)
	defer shell.Close()
	shell.imageCache[avatar] = second
	shell.mediaDesiredImages[avatar] = true
	shell.mediaImageQueued[avatar] = 7
	stamp := imageStamp{modified: modified.UnixNano(), size: testFileSize(t, avatar)}
	shell.applyMediaBatch(mediaResultBatch{images: []mediaImageResult{{
		path: avatar, generation: 7, stamp: stamp, unchanged: true,
	}}})
	if got := shell.imageCache[avatar]; got.ID != second.ID || got.Version != second.Version {
		t.Fatalf("unchanged result replaced bitmap identity: %+v", got)
	}
}

func testFileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func BenchmarkShellWakeAndPaint(b *testing.B) {
	now := time.Now()
	state := domain.State{
		Self: domain.Peer{ID: "self", Name: "我", Online: true},
		Peers: []domain.Peer{
			{ID: "peer-a", Name: "Alice", Online: true},
			{ID: "peer-b", Name: "Bob", Online: true},
		},
		Conversations: []domain.Conversation{
			{ID: "conv-a", Kind: "direct", PeerID: "peer-a", Title: "Alice", UpdatedAt: now, Preview: "hello"},
		},
		Messages: []domain.Message{
			{ID: "message-a", ConversationID: "conv-a", SenderID: "peer-a", Kind: "text", Text: "hello", CreatedAt: now, Status: "delivered"},
		},
		Settings: domain.DefaultSettings(b.TempDir()),
		Revision: 1,
	}
	shell := New(newShellTestBackend(state))
	config := shell.Config()
	driver, err := app.NewDriver(config, nil)
	if err != nil {
		shell.Close()
		b.Fatal(err)
	}
	defer driver.Close()
	defer shell.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		shell.invalidator.Invalidate()
		if err := driver.Pump(); err != nil {
			b.Fatal(err)
		}
	}
}
