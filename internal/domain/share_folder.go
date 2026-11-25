package domain

import (
	"iter"
	"slices"
)

// ShareFolderEntry is a local catalog entry. Path is relative to the shared
// root and uses slash separators. ID and Parent are stable within the share.
type ShareFolderEntry struct {
	ID, Parent int
	Path       string
	Folder     bool
	Size       int64
	Downloads  uint64
}

// ShareFolderContents is immutable and may be reused across snapshots.
type ShareFolderContents struct {
	entries []ShareFolderEntry
}

func NewShareFolderContents(entries []ShareFolderEntry) *ShareFolderContents {
	return &ShareFolderContents{entries: slices.Clone(entries)}
}

func (c *ShareFolderContents) All() iter.Seq[ShareFolderEntry] {
	if c == nil {
		return slices.Values([]ShareFolderEntry(nil))
	}
	return slices.Values(c.entries)
}
