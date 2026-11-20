package domain

import (
	"path/filepath"
	"strings"
)

const AutoVideoLimit int64 = 500 << 20

// Classify by a known container suffix. An untrusted MIME claim cannot
// turn an unrelated attachment into an automatic download.
func (f File) Video() bool {
	if f.Folder || f.ShareFolder {
		return false
	}
	switch strings.ToLower(filepath.Ext(f.Name)) {
	case ".mp4", ".m4v", ".mov", ".mkv", ".webm", ".avi", ".mpg", ".mpeg", ".wmv", ".flv", ".m2ts", ".ogv":
		return true
	}
	return false
}
func (f File) AutoReceiveVideo(s Settings) bool {
	return !s.ManualVideoReceive && !f.Shared && f.Size > 0 && f.Size < AutoVideoLimit && f.Video()
}
