# Offline Linux file dialogs

Both the tar archive and DEB contain `lib/goim/file-dialog`: Zenity 3.44.5
(GTK 3, without WebKit or libnotify), its ELF dependency closure, matching
dynamic loader, an X11 image clipboard helper (xclip), GTK input methods, image decoders, schemas, MIME data and icons.
The system portal and installed zenity/kdialog are preferred. The private helper
is the last PATH entry, used when neither system command is installed. No
package manager or network request is made during installation or use.

Build natively on Debian/Ubuntu, for amd64 or arm64, with:

```sh
sudo apt-get install xclip build-essential meson ninja-build libgtk-3-dev gsettings-desktop-schemas gettext itstool curl xz-utils
bash packaging/linux/build-dialog-runtime.sh /path/to/file-dialog
```

`go run ./cmd/package` runs this builder automatically on Linux. A previously
built runtime can be supplied with `GOIM_LINUX_DIALOG_RUNTIME=/absolute/path`;
the packager checks its architecture. `GOIM_ZENITY_SOURCE` accepts an already
downloaded source archive (the pinned SHA-256 is still checked).

The wrapper runs only the child process with its own loader/library search path.
It does not change GoIM's loader, replace system files, or install a second GTK.
The libraries are ordinary replaceable files. Zenity's source (including the
one-line relocatable gettext change) is in `sources/`. `packages.tsv` records the
exact Debian/Ubuntu binary and source package versions of every bundled library;
`licenses/` contains their distribution copyright/license notices. Release CI
publishes the corresponding dependency source archives alongside the binaries.

Validation uses a private Xvfb display without a portal or system zenity/kdialog:
open, multiple selection, directory selection, save, cancel, Unicode/spaces in
paths, relocated tar contents, and the extracted DEB. Physical Kylin/UOS desktops
and ARM hardware still require native acceptance testing.
