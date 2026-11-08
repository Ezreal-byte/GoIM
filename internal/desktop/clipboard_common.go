package desktop

import (
	"bytes"
	"errors"
)

var screenshotPNGSignature = []byte("\x89PNG\r\n\x1a\n")

func validateClipboardImage(data []byte) error {
	switch {
	case len(data) == 0:
		return errors.New("截图 PNG 为空")
	case len(data) > maxScreenshotBytes:
		return errors.New("截图 PNG 过大")
	case !bytes.HasPrefix(data, screenshotPNGSignature):
		return errors.New("截图不是有效 PNG")
	default:
		return nil
	}
}
