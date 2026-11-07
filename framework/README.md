# Reproducing Viva

Checkout https://github.com/vivaui/viva at the exact commit in viva-commit.txt into the sibling ../viva directory. Apply 0001-global-shortcuts.patch followed by 0002-windows-arm64-text-origin.patch from this directory. App and framework maintain independent Git histories.

The first patch adds optional system-wide shortcut registration for Windows, macOS and Linux/X11, private Driver simulation and native lifecycle tests. Native Wayland returns ErrUnsupported. Product protocol/storage/UI remain in GoIM. The application works without configuring a global shortcut.

The second independent patch fixes a real Windows ARM64 rendering crash: Direct2D's by-value two-float text origin uses a homogeneous floating-point aggregate ABI. It preserves the amd64 packed-integer call and binds an appropriate typed call once per ARM64 render target. Upstream issue: https://github.com/vivaui/viva/issues/156. Regression draws and translates native glyph ink.

Do not replace the pinned commit with a moving branch. After an upstream merge, update the pin and remove the corresponding patch only after rerunning verification.
