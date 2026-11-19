//go:build linux || darwin

package voice

import "unsafe"

func goString(text *byte) string {
	if text == nil {
		return ""
	}
	length := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(text), length)) != 0 {
		length++
	}
	return string(unsafe.Slice(text, length))
}
