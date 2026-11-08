package core

import (
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"

	"goim/internal/domain"
)

const maxInlineImageBytes = 24 << 20

func imageSuffix(mediaType string) string {
	switch mediaType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	default:
		return ""
	}
}

func validInlineImage(file domain.File) bool {
	return file.Size > 0 && file.Size <= maxInlineImageBytes && imageSuffix(file.MIME) != ""
}

// Only actual images with bounded dimensions can take the automatic path.
// Unsupported or oversized content goes through ordinary file confirmation.
func inlineImageType(path string, size int64) string {
	if size <= 0 || size > maxInlineImageBytes {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	config, format, err := image.DecodeConfig(io.LimitReader(f, size))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 8192 || config.Height > 8192 {
		return ""
	}
	maxPixels := int64(16_000_000)
	if format == "png" || format == "gif" {
		maxPixels = 8_000_000
	}
	if int64(config.Width)*int64(config.Height) > maxPixels || imageSuffix("image/"+format) == "" {
		return ""
	}
	return "image/" + format
}

func attachmentPreview(file domain.File) string {
	if file.InlineImage {
		return "[图片]"
	}
	return "[文件] " + file.Name
}
