package desktop

import (
	"errors"
	"os"
	"strings"
	"sync"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

type mediaJobKind uint8

const (
	mediaJobFileAvailability mediaJobKind = iota
	mediaJobImage
	mediaJobLocalAction
)

type mediaJobKey struct {
	kind mediaJobKind
	name string
	id   uint64
}

type mediaJob struct {
	cancel     <-chan struct{}
	key        mediaJobKey
	kind       mediaJobKind
	path       string
	fileID     string
	generation uint64
	imageLimit int

	imagePrevious    imageStamp
	imagePreviousSet bool
	notifyUnchanged  bool

	previousAvailable    bool
	previousAvailableSet bool

	actionID uint64
	reveal   bool
}

type mediaImageResult struct {
	path       string
	generation uint64
	stamp      imageStamp
	bitmap     ui.Bitmap
	decoded    bool
	unchanged  bool
}

type mediaActionResult struct {
	id        uint64
	path      string
	reveal    bool
	available bool
	canceled  bool
	err       string
}

type mediaFileResult struct {
	fileID     string
	path       string
	generation uint64
	available  bool
}

type mediaFileRequest struct {
	path       string
	generation uint64
}

type mediaResultBatch struct {
	files   []mediaFileResult
	images  []mediaImageResult
	actions []mediaActionResult
}

type mediaReadyResults struct {
	files   map[string]mediaFileResult
	images  map[string]mediaImageResult
	actions map[uint64]mediaActionResult
}

func (r *mediaReadyResults) empty() bool {
	return len(r.files) == 0 && len(r.images) == 0 && len(r.actions) == 0
}

// mediaWorker owns every filesystem probe and image decode used by the shell.
// Jobs are merged by their typed key, so a burst of wakes produces one bounded
// batch of work rather than one goroutine per snapshot or paint.
type mediaWorker struct {
	mu sync.Mutex

	pending    map[mediaJobKey]mediaJob
	ready      mediaReadyResults
	processing bool
	closed     bool
	beforeJob  func(mediaJob)

	notify    chan struct{}
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	onReady   func()
}

func newMediaWorker(onReady func()) *mediaWorker {
	worker := &mediaWorker{
		pending: make(map[mediaJobKey]mediaJob),
		ready: mediaReadyResults{
			files:   make(map[string]mediaFileResult),
			images:  make(map[string]mediaImageResult),
			actions: make(map[uint64]mediaActionResult),
		},
		notify:  make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		onReady: onReady,
	}
	go worker.run()
	return worker
}

func (worker *mediaWorker) submit(jobs []mediaJob) {
	if worker == nil || len(jobs) == 0 {
		return
	}
	worker.mu.Lock()
	if worker.closed {
		worker.mu.Unlock()
		return
	}
	for _, job := range jobs {
		worker.pending[job.key] = job
	}
	worker.mu.Unlock()
	select {
	case worker.notify <- struct{}{}:
	default:
	}
}

func (worker *mediaWorker) submitOne(job mediaJob) {
	worker.submit([]mediaJob{job})
}

func (worker *mediaWorker) take() (mediaResultBatch, bool) {
	if worker == nil {
		return mediaResultBatch{}, false
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if worker.ready.empty() {
		return mediaResultBatch{}, false
	}
	batch := mediaResultBatch{
		files:   make([]mediaFileResult, 0, len(worker.ready.files)),
		images:  make([]mediaImageResult, 0, len(worker.ready.images)),
		actions: make([]mediaActionResult, 0, len(worker.ready.actions)),
	}
	for _, result := range worker.ready.files {
		batch.files = append(batch.files, result)
	}
	for _, result := range worker.ready.images {
		batch.images = append(batch.images, result)
	}
	for _, result := range worker.ready.actions {
		batch.actions = append(batch.actions, result)
	}
	worker.ready = mediaReadyResults{
		files:   make(map[string]mediaFileResult),
		images:  make(map[string]mediaImageResult),
		actions: make(map[uint64]mediaActionResult),
	}
	return batch, true
}

type mediaProcessingResult struct {
	kind   mediaJobKind
	job    mediaJob
	file   mediaFileResult
	image  mediaImageResult
	action mediaActionResult
	notify bool
}

func (worker *mediaWorker) run() {
	defer close(worker.done)
	for {
		select {
		case <-worker.stop:
			return
		default:
		}
		select {
		case <-worker.stop:
			return
		case <-worker.notify:
		}

		worker.mu.Lock()
		if worker.closed {
			worker.mu.Unlock()
			return
		}
		jobs := make([]mediaJob, 0, len(worker.pending))
		for _, job := range worker.pending {
			jobs = append(jobs, job)
		}
		worker.pending = make(map[mediaJobKey]mediaJob)
		beforeJob := worker.beforeJob
		worker.processing = true
		worker.mu.Unlock()

		results := make([]mediaProcessingResult, 0, len(jobs))
		stopped := false
		for _, job := range jobs {
			select {
			case <-worker.stop:
				stopped = true
			default:
			}
			if stopped {
				break
			}
			if beforeJob != nil {
				beforeJob(job)
			}
			job.cancel = worker.stop
			results = append(results, processMediaJob(job))
		}

		worker.mu.Lock()
		worker.processing = false
		if worker.closed || stopped {
			worker.mu.Unlock()
			return
		}
		notify := false
		for _, result := range results {
			switch result.kind {
			case mediaJobFileAvailability:
				worker.ready.files[result.file.fileID] = result.file
			case mediaJobImage:
				worker.ready.images[result.image.path] = result.image
			case mediaJobLocalAction:
				worker.ready.actions[result.action.id] = result.action
			}
			notify = notify || result.notify
		}
		worker.mu.Unlock()
		if notify && worker.onReady != nil {
			worker.onReady()
		}
	}
}

func (worker *mediaWorker) close() {
	if worker == nil {
		return
	}
	worker.closeOnce.Do(func() {
		worker.mu.Lock()
		worker.closed = true
		worker.pending = nil
		worker.ready = mediaReadyResults{}
		worker.mu.Unlock()
		close(worker.stop)
	})
}

func (worker *mediaWorker) settled() bool {
	if worker == nil {
		return true
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	return !worker.processing && len(worker.pending) == 0
}

func (worker *mediaWorker) setBeforeJob(hook func(mediaJob)) {
	if worker == nil {
		return
	}
	worker.mu.Lock()
	worker.beforeJob = hook
	worker.mu.Unlock()
}

func processMediaJob(job mediaJob) mediaProcessingResult {
	switch job.kind {
	case mediaJobFileAvailability:
		available, _ := probeMediaFile(job.path)
		return mediaProcessingResult{
			kind: mediaJobFileAvailability,
			job:  job,
			file: mediaFileResult{
				fileID: job.fileID, path: job.path, generation: job.generation, available: available,
			},
			notify: !job.previousAvailableSet || job.previousAvailable != available,
		}
	case mediaJobImage:
		return processMediaImage(job)
	case mediaJobLocalAction:
		available, errText := probeMediaFile(job.path)
		canceled := false
		if available {
			select {
			case <-job.cancel:
				return mediaProcessingResult{kind: job.kind, job: job, action: mediaActionResult{id: job.actionID, path: job.path, reveal: job.reveal, canceled: true}}
			default:
			}
			var err error
			if job.reveal {
				err = app.RevealPath(job.path)
			} else {
				err = app.OpenPath(job.path)
			}
			switch {
			case errors.Is(err, app.ErrCanceled):
				canceled = true
			case err != nil:
				errText = err.Error()
			}
		}
		return mediaProcessingResult{
			kind: mediaJobLocalAction,
			job:  job,
			action: mediaActionResult{
				id: job.actionID, path: job.path, reveal: job.reveal,
				available: available, canceled: canceled, err: errText,
			},
			notify: true,
		}
	default:
		return mediaProcessingResult{kind: job.kind, job: job}
	}
}

func processMediaImage(job mediaJob) mediaProcessingResult {
	limit := job.imageLimit
	if limit <= 0 {
		limit = 512
	}
	missing := imageStamp{modified: -1}
	stamp := missing
	info, err := os.Stat(job.path)
	if err == nil && info != nil {
		stamp = imageStamp{modified: info.ModTime().UnixNano(), size: info.Size()}
	}
	if job.imagePreviousSet && job.imagePrevious == stamp {
		return mediaProcessingResult{
			kind: mediaJobImage,
			job:  job,
			image: mediaImageResult{
				path: job.path, generation: job.generation, stamp: stamp, unchanged: true,
			},
			notify: job.notifyUnchanged,
		}
	}
	result := mediaProcessingResult{
		kind: mediaJobImage,
		job:  job,
		image: mediaImageResult{
			path: job.path, generation: job.generation, stamp: stamp,
		},
		notify: true,
	}
	if err != nil || info == nil || info.IsDir() {
		return result
	}
	result.image.bitmap, result.image.decoded = decodeBitmap(job.path, limit)
	return result
}

func probeMediaFile(path string) (bool, string) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err.Error()
	}
	if info == nil || info.IsDir() {
		return false, "路径不是普通文件"
	}
	return true, ""
}

// localFilePaths resolves the same local source for the shell and the panels.
// A completed receive uses the most recently updated transfer path.
func localFilePaths(state domain.State) map[string]string {
	latestReceive := make(map[string]domain.Transfer)
	for _, transfer := range state.Transfers {
		path := strings.TrimSpace(transfer.Path)
		if transfer.Direction != "receive" || transfer.Status != "completed" ||
			transfer.FileID == "" || path == "" {
			continue
		}
		previous, ok := latestReceive[transfer.FileID]
		if !ok || !transfer.UpdatedAt.Before(previous.UpdatedAt) {
			latestReceive[transfer.FileID] = transfer
		}
	}
	paths := make(map[string]string, len(state.Files))
	for _, file := range state.Files {
		path := strings.TrimSpace(file.SourcePath)
		if path == "" {
			if transfer, ok := latestReceive[file.ID]; ok {
				path = strings.TrimSpace(transfer.Path)
			}
		}
		if path != "" {
			paths[file.ID] = path
		}
	}
	return paths
}
