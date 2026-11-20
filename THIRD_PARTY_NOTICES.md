# Third party notices

Copyright (c) 2026 Ezreal-byte. GoIM is licensed under the [Apache License, Version 2.0](LICENSE). Third-party components are distributed under their respective licenses, included in `licenses/`.

| Material | Version / source | License text |
| --- | --- | --- |
| Viva | See go.mod | licenses/Viva-Apache-2.0.txt, Viva-NOTICE.txt, Viva-third-party.md |
| bbolt | 1.5.0 | licenses/bbolt-MIT.txt |
| purego | 0.11.1 | licenses/purego-Apache-2.0.txt |
| x/sys | 0.45.0 | licenses/x-sys-BSD.txt |
| x/text | 0.36.0 | licenses/x-text.txt |
| kbinani/screenshot (companion) | 089614a94018 | licenses/kbinani-screenshot.txt |
| gen2brain/shm (Linux companion) | 0.1.0 | licenses/gen2brain-shm.txt |
| godbus/dbus (Linux companion) | 5.1.0 | licenses/godbus-dbus.txt |
| jezek/xgb (Linux companion) | 1.1.1 | licenses/jezek-xgb.txt |
| Go runtime / standard library | 1.27.3 | licenses/Go-BSD.txt |
| Material Icon Theme file-type icons | Material Extensions, via Iconify and Motty. The PowerPoint icon comes directly from Material Icon Theme. | licenses/material-icon-theme-MIT.txt |
| Lucide compiled icons and application icon geometry | Lucide SVG sources compiled by Viva | Lucide ISC and Feather MIT sections in licenses/Viva-third-party.md |
| Lucide icons in the browser share page | [0.468.0](https://github.com/lucide-icons/lucide/tree/0.468.0) | licenses/lucide-web-ISC.txt |

The Viva notice also preserves upstream notices for its Unicode and font-related material. Native system fonts and graphics libraries are supplied by the operating system; font files are not bundled by GoIM.

Browser share QR codes are generated with [go-qrcode](https://github.com/skip2/go-qrcode), covered by [its bundled license](licenses/go-qrcode-LICENSE.txt).

Windows and macOS packages include the Shotlet capture SDK helper under Apache-2.0, with SDL (zlib), Dear ImGui (MIT), and nlohmann/json (MIT). The packages include the full license texts and upstream third-party notices under `licenses/shotlet/`. The source revisions are pinned in `packaging/shotlet.cmake`. [Shotlet source](https://github.com/Ezreal-byte/shotlet)

## Ocean sound theme

The message notification and incoming-call recordings are from [KDE Ocean](https://github.com/KDE/ocean-sound-theme/tree/66ee3d17a8ff1ba0fd5c76303a7b860a19542c79/ocean/stereo). Copyright 2023 Guilherme Marçal Silva. The original recordings and these adaptations are licensed under [CC BY-SA 4.0](licenses/Ocean-CC-BY-SA-4.0.txt). GoIM converts them to 16 kHz mono PCM, trims the leading silence and pads the tail to a playback frame.

## FFmpeg video decoder

GoIM runs an independent, offline build of [FFmpeg 8.1.3](https://ffmpeg.org/releases/ffmpeg-8.1.3.tar.xz) for video playback. FFmpeg is copyright its contributors and this build uses LGPL-2.1-or-later components. The unchanged FFmpeg source archive, license text and build script are distributed in `licenses/ffmpeg` (inside the application resources on macOS). You may rebuild and replace the separate `goim-media` executable.
