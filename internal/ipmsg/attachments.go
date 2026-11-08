package ipmsg

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

type Attachment struct {
	ID      uint32
	Name    string
	Size    int64
	ModTime int64
	Kind    uint32
}

func encodeAttachments(files []Attachment) string {
	encoded := make([]string, 0, len(files))
	for _, file := range files {
		name := strings.ReplaceAll(file.Name, ":", "::")
		name = strings.ReplaceAll(name, "\a", "_")
		encoded = append(encoded, fmt.Sprintf("%d:%s:%x:%x:%x:\a",
			file.ID, name, file.Size, file.ModTime, file.Kind))
	}
	return strings.Join(encoded, "")
}

func decodeAttachments(value string) []Attachment {
	entries := strings.Split(value, "\a")
	files := make([]Attachment, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		// A filename escapes ':' as '::'. Replace it before splitting the
		// protocol fields, then restore the literal colon.
		fields := strings.Split(strings.ReplaceAll(entry, "::", "\x01"), ":")
		if len(fields) < 5 {
			continue
		}
		// Offer IDs are decimal; GETFILEDATA encodes the same ID in hex.
		id, errID := strconv.ParseUint(fields[0], 10, 32)
		size, errSize := strconv.ParseInt(fields[2], 16, 64)
		mtime, _ := strconv.ParseInt(fields[3], 16, 64)
		kind, errKind := strconv.ParseUint(fields[4], 16, 32)
		name := decodeLegacyString([]byte(strings.ReplaceAll(fields[1], "\x01", ":")))
		if errID != nil || errSize != nil || errKind != nil || kind&0xff != fileRegular || size < 0 || name == "" {
			continue
		}
		files = append(files, Attachment{
			ID: uint32(id), Name: safeLegacyName(name), Size: size,
			ModTime: mtime, Kind: uint32(kind),
		})
	}
	return files
}

func safeLegacyName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(strings.TrimSpace(name), ". ")
	if name == "" || name == "." || name == ".." {
		return "legacy-file"
	}
	const maxRunes = 180
	if runes := []rune(name); len(runes) > maxRunes {
		ext := filepath.Ext(name)
		if n := len([]rune(ext)); n < maxRunes {
			name = string(runes[:maxRunes-n]) + ext
		} else {
			name = string(runes[:maxRunes])
		}
	}
	return name
}

func parseFileRequest(extra string) (packetNumber uint64, fileID uint32, offset int64, err error) {
	fields := strings.Split(extra, ":")
	if len(fields) != 3 {
		return 0, 0, 0, errors.New("invalid IPMSG file request")
	}
	packetNumber64, err := strconv.ParseUint(fields[0], 16, 64)
	if err != nil {
		return 0, 0, 0, err
	}
	fileID64, err := strconv.ParseUint(fields[1], 16, 32)
	if err != nil {
		return 0, 0, 0, err
	}
	offset64, err := strconv.ParseInt(fields[2], 16, 64)
	if err != nil || offset64 < 0 {
		return 0, 0, 0, errors.New("invalid IPMSG file offset")
	}
	return packetNumber64, uint32(fileID64), offset64, nil
}
