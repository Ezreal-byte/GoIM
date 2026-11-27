# Shotlet capture helper

GoIM builds the SDK helper from the immutable source revision in `packaging/shotlet.cmake`.
The standalone Shotlet package does not contain this executable or its host protocol.

Linux tarballs place it in `lib/goim/shotlet/shotlet-capture`; DEB installs use
`/usr/lib/goim/shotlet/shotlet-capture`. Resources stay beside the executable and
licenses are included in the GoIM package. The existing private file-dialog
runtime provides Zenity when the desktop has no chooser.

Linux builds require CMake, Ninja, the upstream X11/Fontconfig development
dependencies, and Zig 0.15.2. Run `bash packaging/shotlet/install-zig.sh CACHE`
and add the printed toolchain directory to PATH. The helper targets glibc 2.28
and links libc++ statically; the whole GoIM package retains its glibc 2.31 baseline.

X11 uses Shotlet, including its software renderer when Vulkan cannot initialize.
Wayland is currently unsupported by upstream. A missing helper or a failed
protocol self-test permits the built-in fallback. Execution-policy refusal,
capture failure, user cancellation and save-only completion do not start another
capture. `GOIM_SNIP_PATH` remains an explicit development override.

The release workflow runs `test-sdk.py` against the actual staged helper on an
isolated Xvfb desktop, with normal rendering and with Vulkan disabled. It checks
captured pixels, annotations, Chinese text, cancellation and save-only completion.
Run it with `--helper PATH --source PINNED_SHOTLET_CHECKOUT --artifacts DIRECTORY`;
the display must be 1280x800x24. The test adds the installed sibling file-dialog
runtime to PATH, as GoIM does at startup. For a development helper outside the
package layout, pass `--dialog-bin PRIVATE_RUNTIME/bin` explicitly.
It never touches the user's real desktop.
