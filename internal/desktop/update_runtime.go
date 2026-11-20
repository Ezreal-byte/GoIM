package desktop

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/vivaui/viva/update"

	"goim/internal/selfupdate"
)

type updatePhase uint8

const (
	updateDisabled updatePhase = iota
	updateIdle
	updateChecking
	updateCurrent
	updateOffered
	updateDownloading
	updateReady
	updateInstalling
	updateCheckFailed
	updateDownloadFailed
)

type updateView struct {
	phase         updatePhase
	offer         selfupdate.Offer
	notes         string
	notesProblem  string
	notesLoading  bool
	progress      float32
	problem       string
	previous      string
	success, quit bool
}

func (s updateView) busy() bool {
	return s.phase == updateChecking || s.phase == updateDownloading || s.phase == updateInstalling
}

type updateClient interface {
	Check(context.Context) (selfupdate.Offer, error)
	ReleaseNotes(context.Context, selfupdate.Offer) (string, error)
	Download(context.Context, selfupdate.Offer, func(int64, int64)) (string, error)
	Install(selfupdate.Offer, string, []string, func()) error
}

type updateController struct {
	mu                 sync.Mutex
	view               updateView
	client             updateClient
	automatic          bool
	artifact           string
	ctx                context.Context
	stop, stopDownload context.CancelFunc
	wake               func()
	scheduleWake       chan struct{}
	nextCheck          time.Time
	checkInterval      time.Duration
	retryInterval      time.Duration
}

const automaticUpdateInterval = 6 * time.Hour
const automaticUpdateRetry = 30 * time.Minute

func newUpdateController(version, key string, automatic bool, wake func()) *updateController {
	ctx, stop := context.WithCancel(context.Background())
	u := &updateController{ctx: ctx, stop: stop, wake: wake, automatic: automatic,
		scheduleWake: make(chan struct{}, 1), checkInterval: automaticUpdateInterval, retryInterval: automaticUpdateRetry}
	if key == "" {
		return u
	}
	if automatic {
		u.view.phase = updateChecking
	}
	go u.runAutomatic()
	go func() {
		client, err := selfupdate.Open(version, key)
		if err != nil || client == nil {
			u.change(func(v *updateView) {
				v.phase, v.problem = updateDisabled, "当前构建无法检查更新"
			})
			return
		}
		previous, succeeded := client.PreviousInstall()
		u.mu.Lock()
		u.client = client
		u.view.previous, u.view.success = previous, succeeded
		automatic := u.automatic
		if automatic {
			u.view.phase = updateChecking
		} else {
			u.view.phase = updateIdle
		}
		u.mu.Unlock()
		if automatic {
			u.mu.Lock()
			u.nextCheck = time.Now().Add(u.checkInterval)
			u.mu.Unlock()
			u.check()
		} else {
			u.wake()
		}
		u.reschedule()
	}()
	return u
}

func (u *updateController) setAutomatic(enabled bool) {
	if u == nil {
		return
	}
	u.mu.Lock()
	previous := u.automatic
	u.automatic = enabled
	u.mu.Unlock()
	if enabled == previous {
		return
	}
	if enabled && !previous {
		u.beginCheck(true)
	}
	u.reschedule()
}

func (u *updateController) reschedule() {
	select {
	case u.scheduleWake <- struct{}{}:
	default:
	}
}

func (u *updateController) runAutomatic() {
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		u.mu.Lock()
		enabled := u.automatic && u.client != nil && !u.view.busy() && u.view.phase != updateReady
		next := u.nextCheck
		u.mu.Unlock()
		var due <-chan time.Time
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		if enabled {
			timer.Reset(max(time.Until(next), 0))
			due = timer.C
		}
		select {
		case <-u.ctx.Done():
			return
		case <-u.scheduleWake:
		case <-due:
			u.beginCheck(true)
		}
	}
}

func (u *updateController) snapshot() updateView {
	if u == nil {
		return updateView{}
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.view
}

func (u *updateController) change(apply func(*updateView)) {
	u.mu.Lock()
	apply(&u.view)
	u.mu.Unlock()
	u.wake()
	u.reschedule()
}

func (u *updateController) checkNow() {
	u.beginCheck(false)
}

func (u *updateController) beginCheck(automatic bool) {
	u.mu.Lock()
	if u.client == nil || u.view.busy() || automatic && (!u.automatic || u.view.phase == updateReady) || u.ctx.Err() != nil {
		u.mu.Unlock()
		return
	}
	u.view.phase, u.view.problem = updateChecking, ""
	interval := u.checkInterval
	if interval <= 0 {
		interval = automaticUpdateInterval
	}
	u.nextCheck = time.Now().Add(interval)
	u.mu.Unlock()
	u.wake()
	go u.check()
}

func (u *updateController) check() {
	defer u.reschedule()
	offer, err := u.client.Check(u.ctx)
	if u.ctx.Err() != nil {
		return
	}
	if err != nil {
		u.mu.Lock()
		interval := u.retryInterval
		if interval <= 0 {
			interval = automaticUpdateRetry
		}
		u.nextCheck = time.Now().Add(interval)
		u.mu.Unlock()
	}
	u.change(func(v *updateView) {
		v.progress = 0
		v.notes, v.notesProblem, v.notesLoading = "", "", false
		switch {
		case err != nil:
			v.phase, v.problem, v.offer = updateCheckFailed, updateCheckProblem(err), selfupdate.Offer{}
		case offer.Version == "":
			v.phase, v.problem, v.offer = updateCurrent, "", selfupdate.Offer{}
		default:
			v.phase, v.problem, v.offer, v.notesLoading = updateOffered, "", offer, !offer.FeedUnavailable
		}
	})
	if err != nil || offer.Version == "" || offer.FeedUnavailable {
		return
	}
	notesCtx, cancelNotes := context.WithTimeout(u.ctx, 8*time.Second)
	defer cancelNotes()
	notes, notesErr := u.client.ReleaseNotes(notesCtx, offer)
	u.change(func(v *updateView) {
		// Download and installation can advance while the independent notes
		// request is pending. They still describe the same offered version.
		if v.offer.Version != offer.Version {
			return
		}
		v.notesLoading = false
		if notesErr != nil {
			v.notesProblem = "暂时无法加载发布说明"
			return
		}
		v.notes = notes
	})
}

func updateCheckProblem(err error) string {
	var networkError net.Error
	switch {
	case errors.Is(err, update.ErrSignature):
		return updateProblem(err, "")
	case errors.Is(err, update.ErrManifest), errors.Is(err, update.ErrInsecureURL), errors.Is(err, update.ErrTooLarge), errors.Is(err, selfupdate.ErrUpdateSource):
		return "无法验证更新源，请查看官方发布页"
	case errors.Is(err, selfupdate.ErrUpdateRate):
		return "GitHub 暂时限制更新请求，请稍后重试"
	case errors.Is(err, context.DeadlineExceeded):
		return "连接 GitHub 超时，请检查网络或代理"
	case errors.Is(err, selfupdate.ErrUpdateHTTP):
		return "GitHub 更新源暂不可用，请稍后重试"
	case errors.As(err, &networkError):
		return "无法连接 GitHub，请检查网络或代理"
	default:
		return "暂时无法检查更新"
	}
}

func (u *updateController) download() {
	u.mu.Lock()
	offer := u.view.offer
	if u.client == nil || !offer.Automatic || u.view.phase != updateOffered && u.view.phase != updateDownloadFailed {
		u.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(u.ctx)
	u.stopDownload = cancel
	u.view.phase, u.view.progress, u.view.problem = updateDownloading, 0, ""
	u.mu.Unlock()
	u.wake()
	go func() {
		defer cancel()
		var last float32 = -1
		artifact, err := u.client.Download(ctx, offer, func(done, total int64) {
			if total <= 0 {
				return
			}
			value := max(0, min(1, float32(done)/float32(total)))
			if value == 1 || value-last >= .02 {
				last = value
				u.change(func(v *updateView) { v.progress = value })
			}
		})
		u.change(func(v *updateView) {
			u.stopDownload = nil
			switch {
			case ctx.Err() != nil:
				v.phase, v.problem = updateOffered, "已暂停下载，可继续"
			case err != nil:
				v.phase, v.problem = updateDownloadFailed, updateProblem(err, "下载未完成，请重试")
			default:
				u.artifact = artifact
				v.phase, v.problem, v.progress = updateReady, "", 1
			}
		})
	}()
}

func (u *updateController) pauseDownload() {
	u.mu.Lock()
	stop := u.stopDownload
	u.mu.Unlock()
	if stop != nil {
		stop()
	}
}

func (u *updateController) install(args []string) {
	u.mu.Lock()
	if u.client == nil || u.view.phase != updateReady {
		u.mu.Unlock()
		return
	}
	offer, artifact := u.view.offer, u.artifact
	u.view.phase = updateInstalling
	u.mu.Unlock()
	u.wake()
	go func() {
		if err := u.client.Install(offer, artifact, args, func() {
			u.change(func(v *updateView) { v.quit = true })
		}); err != nil {
			u.change(func(v *updateView) {
				v.phase, v.problem = updateDownloadFailed, updateProblem(err, "未能启动安装，请重试或使用官方下载")
			})
		}
	}()
}

func updateProblem(err error, fallback string) string {
	switch {
	case errors.Is(err, update.ErrSignature):
		return "更新清单的签名无法验证"
	case errors.Is(err, update.ErrChecksum), errors.Is(err, update.ErrSize):
		return "下载的软件包校验失败，请重新下载"
	case errors.Is(err, update.ErrUntrusted):
		return "系统无法验证安装包，请使用官方下载"
	case errors.Is(err, update.ErrNotWritable), errors.Is(err, update.ErrManagedExternally):
		return "此安装无法原位升级，请使用官方下载"
	default:
		return fallback
	}
}
