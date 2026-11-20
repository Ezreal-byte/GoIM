package desktop

import (
	"errors"

	"github.com/vivaui/viva/app"
	"goim/internal/i18n"
)

func filePickerProblem(err error) string {
	if errors.Is(err, app.ErrUnsupported) {
		return i18n.T("系统文件选择器暂不可用，请通过系统软件中心修复或重新安装 GoIM，安装器会自动补齐所需组件。")
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

// A native file dialog pumps the window's event queue before returning. If it
// re-enters the same click pass, that pass must not open a second dialog.
func (u *UI) openFilePicker(options app.OpenFileOptions) ([]string, error) {
	if u.filePickerOpen {
		return nil, app.ErrCanceled
	}
	u.filePickerOpen = true
	defer func() { u.filePickerOpen = false }()
	return app.OpenFiles(options)
}
