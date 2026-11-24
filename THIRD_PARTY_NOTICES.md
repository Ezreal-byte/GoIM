# Third party notices

Copyright (c) 2026 Ezreal-byte. GoIM is licensed under the [Apache License, Version 2.0](LICENSE). Third-party components are distributed under their respective licenses, included in `licenses/`.

| Material | Version / source | License text |
| --- | --- | --- |
| LocalSend logo artwork | Black and white logo assets from [LocalSend v1.18.2](https://github.com/localsend/localsend/tree/v1.18.2/app/assets/img). Copyright 2022–2026 Tien Do Nam | [Apache-2.0](licenses/LocalSend-Apache-2.0.txt) |
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
| Microsoft Fluent Emoji 3D artwork | [1ffb34c752ec](https://github.com/microsoft/fluentui-emoji/tree/1ffb34c752ecf5d402f04cfb4b392c77f57c54bc), scaled for the picker and Linux fallback font | licenses/fluent-emoji-MIT.txt |

The Viva notice also preserves upstream notices for its Unicode and font-related material. GoIM uses native system fonts and graphics libraries and embeds the Fluent Emoji fallback font on Linux.

Browser share QR codes are generated with [go-qrcode](https://github.com/skip2/go-qrcode), covered by [its bundled license](licenses/go-qrcode-LICENSE.txt).

Windows and macOS packages include the Shotlet capture SDK helper under Apache-2.0, with SDL (zlib), Dear ImGui (MIT), and nlohmann/json (MIT). The packages include the full license texts and upstream third-party notices under `licenses/shotlet/`. The source revisions are pinned in `packaging/shotlet.cmake`. [Shotlet source](https://github.com/Ezreal-byte/shotlet)

Linux packages also include xclip for X11 image copying. Its distribution license and corresponding source are included with the Linux file dialog runtime and its source archive.

## FFmpeg video decoder

GoIM runs an independent, offline build of [FFmpeg 8.1.3](https://ffmpeg.org/releases/ffmpeg-8.1.3.tar.xz) for video playback. FFmpeg is copyright its contributors and this build uses LGPL-2.1-or-later components. The unchanged FFmpeg source archive, license text and build script are distributed in `licenses/ffmpeg` (inside the application resources on macOS). You may rebuild and replace the separate `goim-media` executable.
