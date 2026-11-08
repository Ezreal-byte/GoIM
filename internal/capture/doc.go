// Package capture provides cross-platform display geometry, screen capture, and
// a portable screenshot annotation document.
//
// Windows uses native display capture; Linux and cgo-enabled macOS use the
// screenshot adapter. On unsupported targets the annotation API still works.
// Monitors left of or above the primary monitor can have negative coordinates.
//
// The annotation document and PNG export are platform-independent and can be
// used with screenshots loaded from any image source.
package capture
