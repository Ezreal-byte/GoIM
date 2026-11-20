// Package fileicon classifies shared files for the desktop client and browser.
package fileicon

import (
	"path"
	"strings"
)

var byName = map[string]string{
	".gitattributes":      "git",
	".gitconfig":          "git",
	".gitignore":          "git",
	".gitmodules":         "git",
	"copying":             "license",
	"docker-compose.yaml": "docker",
	"docker-compose.yml":  "docker",
	"dockerfile":          "docker",
	"licence":             "license",
	"license":             "license",
	"makefile":            "settings",
	"nginx.conf":          "nginx",
	"package-lock.json":   "nodejs",
	"package.json":        "nodejs",
	"readme":              "readme",
	"readme.md":           "readme",
	"readme.txt":          "readme",
}

var byExtension = func() map[string]string {
	result := make(map[string]string)
	for kind, extensions := range map[string]string{
		"archive":     "7z bz2 cab gz iso jar lz rar tar tgz war xz zip zst",
		"audio":       "aac flac m4a mp3 ogg opus wav wma",
		"c":           "c h",
		"certificate": "cer crt csr p12 pem pfx",
		"console":     "bash bat cmd fish sh zsh",
		"cpp":         "cc cpp hpp",
		"csharp":      "cs",
		"css":         "css less",
		"database":    "db sql sqlite sqlite3",
		"document":    "doc docx odt pages rst rtf tex txt wps",
		"exe":         "apk app appimage deb dmg exe msi pkg rpm",
		"go":          "go",
		"gradle":      "gradle",
		"html":        "htm html xhtml",
		"image":       "avif bmp gif heic ico jfif jpeg jpg png svg tif tiff webp",
		"java":        "java",
		"javascript":  "cjs js mjs",
		"json":        "json json5 jsonc jsonl ndjson",
		"key":         "asc gpg p8 pub",
		"lock":        "lock",
		"log":         "log",
		"lua":         "lua",
		"markdown":    "markdown md mdx",
		"pdf":         "pdf",
		"php":         "php",
		"powerpoint":  "dps key odp pps ppsx ppt pptx",
		"powershell":  "ps1",
		"python":      "py",
		"react":       "jsx tsx",
		"ruby":        "rb",
		"rust":        "rs",
		"sass":        "sass scss",
		"settings":    "cfg conf env ini properties",
		"swift":       "swift",
		"table":       "csv et numbers ods tsv xls xlsm xlsx",
		"toml":        "toml",
		"typescript":  "ts",
		"video":       "avi flv m4v mkv mov mp4 mpeg mpg webm wmv",
		"vue":         "vue",
		"xml":         "kt plist xml",
		"yaml":        "yaml yml",
	} {
		for _, ext := range strings.Fields(extensions) {
			result["."+ext] = kind
		}
	}
	return result
}()

// Kind follows Motty’s built-in SFTP glyphs for names and extensions. MIME
// supplies a fallback for attachments whose names do not indicate their type.
func Kind(name, mediaType string, folder bool) string {
	name = strings.ToLower(path.Base(name))
	ext := path.Ext(name)
	if folder {
		return "folder"
	}
	if kind := byName[name]; kind != "" {
		return kind
	}
	if kind := byExtension[ext]; kind != "" {
		return kind
	}
	if strings.HasPrefix(name, ".") && name != "." && name != ".." {
		return "settings"
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
	case "text/html", "application/xhtml+xml":
		return "html"
	case "text/css":
		return "css"
	case "text/javascript", "application/javascript":
		return "javascript"
	case "application/yaml", "text/yaml", "application/x-yaml":
		return "yaml"
	case "application/toml":
		return "toml"
	case "application/xml", "text/xml":
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
