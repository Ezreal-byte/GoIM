package desktop

import "github.com/vivaui/viva/app"

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
