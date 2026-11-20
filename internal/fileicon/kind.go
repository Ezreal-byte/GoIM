// Package fileicon classifies shared files for the desktop client and browser.
package fileicon

import (
	"path"
	"strings"
)

var byExtension = func() map[string]string {
	result := make(map[string]string)
	for kind, extensions := range map[string]string{
		"document":   "doc docx odt rtf txt wps pages",
		"table":      "xls xlsx xlsm ods csv tsv et numbers",
		"powerpoint": "ppt pptx pps ppsx odp dps key",
		"pdf":        "pdf",
		"markdown":   "md mdx markdown",
		"archive":    "zip 7z rar tar gz bz2 xz tgz zst lz cab",
		"image":      "jpg jpeg jfif png gif bmp webp avif svg ico heic tiff tif",
		"audio":      "mp3 wav flac m4a aac ogg opus wma",
		"video":      "mp4 mkv avi mov webm wmv mpg mpeg m4v",
		"console":    "sh bash zsh ps1 cmd bat",
		"xml":        "go py js ts jsx tsx rs c h cpp hpp cs java kt swift php rb html css xml",
		"json":       "json jsonl",
		"settings":   "yaml yml ini toml cfg conf",
		"database":   "db sqlite sqlite3 sql",
		"exe":        "exe msi apk deb rpm pkg dmg appimage",
	} {
		for _, ext := range strings.Fields(extensions) {
			result["."+ext] = kind
		}
	}
	return result
}()

// Kind returns the built-in icon for a file. Folders take priority because a
// folder name can also have an extension.
func Kind(name, mediaType string, folder bool) string {
	if folder {
		return "folder"
	}
	if kind := byExtension[strings.ToLower(path.Ext(name))]; kind != "" {
		return kind
	}
	mt, _, _ := strings.Cut(mediaType, ";")
	mt = strings.ToLower(strings.TrimSpace(mt))
	switch mt {
	case "application/pdf":
		return "pdf"
	case "text/markdown":
		return "markdown"
	case "text/csv", "text/tab-separated-values", "application/vnd.ms-excel", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "application/vnd.oasis.opendocument.spreadsheet":
		return "table"
	case "application/vnd.ms-powerpoint", "application/vnd.openxmlformats-officedocument.presentationml.presentation", "application/vnd.oasis.opendocument.presentation":
		return "powerpoint"
	case "application/msword", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/vnd.oasis.opendocument.text":
		return "document"
	case "application/zip", "application/x-7z-compressed", "application/gzip", "application/x-tar":
		return "archive"
	case "application/json":
		return "json"
	case "application/xml", "text/xml", "text/html", "text/css":
		return "xml"
	}
	family, _, _ := strings.Cut(mt, "/")
	switch family {
	case "image", "audio", "video":
		return family
	case "text":
		return "document"
	}
	return "file"
}
