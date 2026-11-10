package desktop

import (
	"bytes"
	"errors"
	"goim/internal/i18n"
)

var screenshotPNGSignature = []byte("\x89PNG\r\n\x1a\n")

func validateClipboardImage(data []byte) error {
	switch {
	case len(data) == 0:
		return errors.New(i18n.T("截图 PNG 为空"))
	case len(data) > maxScreenshotBytes:
		return errors.New(i18n.T("截图 PNG 过大"))
	case !bytes.HasPrefix(data, screenshotPNGSignature):
		return errors.New(i18n.T("截图不是有效 PNG"))
	default:
		return nil
	}
}
