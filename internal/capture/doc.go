// Package capture provides cross-platform display geometry, screen capture, and
// a portable screenshot annotation document.
//
// Native screen capture is implemented on Windows. Other targets compile and
// expose the same API but return ErrUnsupported from capture calls. Display
// bounds are in physical desktop pixels; a monitor left of or above the primary
// monitor therefore has negative coordinates.
//
// The annotation document and PNG export are platform-independent and can be
// used with screenshots loaded from any image source.
package capture
