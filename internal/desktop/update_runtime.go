package desktop

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/vivaui/viva/update"

	"goim/internal/selfupdate"
)

type updatePhase uint8

const (
	updateDisabled updatePhase = iota
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
	Download(context.Context, selfupdate.Offer, func(int64, int64)) (string, error)
	Install(selfupdate.Offer, string, []string, func()) error
}

type updateController struct {
	mu                 sync.Mutex
	view               updateView
	client             updateClient
	artifact           string
	ctx                context.Context
	stop, stopDownload context.CancelFunc
	wake               func()
}

func newUpdateController(version, key string, wake func()) *updateController {
	ctx, stop := context.WithCancel(context.Background())
	u := &updateController{ctx: ctx, stop: stop, wake: wake}
	if key == "" {
		return u
	}
	u.view.phase = updateChecking
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
		u.mu.Unlock()
		u.check()
	}()
	return u
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
}

func (u *updateController) checkNow() {
	u.mu.Lock()
	if u.client == nil || u.view.busy() {
		u.mu.Unlock()
		return
	}
	u.view.phase, u.view.problem = updateChecking, ""
	u.mu.Unlock()
	u.wake()
	go u.check()
}

func (u *updateController) check() {
	ctx, cancel := context.WithTimeout(u.ctx, 20*time.Second)
	defer cancel()
	offer, err := u.client.Check(ctx)
	u.change(func(v *updateView) {
		v.progress = 0
		switch {
		case err != nil:
			v.phase, v.problem, v.offer = updateCheckFailed, updateProblem(err, "暂时无法检查更新"), selfupdate.Offer{}
		case offer.Version == "":
			v.phase, v.problem, v.offer = updateCurrent, "", selfupdate.Offer{}
		default:
			v.phase, v.problem, v.offer = updateOffered, "", offer
		}
	})
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
