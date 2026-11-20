<!-- Desktop screenshots use Viva's headless renderer. The web screenshot uses a headless browser connected to GoIM's sharing service. Both use demo data. -->
<div align="center">
  <p>English · <a href="README.md">简体中文</a></p>
  <img src="website/assets/goim.png" width="84" height="84" alt="GoIM logo" />
  <h1>GoIM</h1>
  <p><strong>Local network messaging and file sharing</strong></p>
  <p>
    <a href="https://ezreal-byte.github.io/GoIM/en/">Website</a> ·
    <a href="https://github.com/Ezreal-byte/GoIM/releases/latest">Download</a> ·
    <a href="#quick-start">Quick start</a> ·
    <a href="#legacy-compatibility">Compatibility</a>
  </p>
  <a href="website/assets/screenshots/chat-dark.png">
    <img src="website/assets/screenshots/chat-dark.png" alt="GoIM group chat in dark mode with an inline image and the chat files panel" width="100%" />
  </a>
  <p><sub>The native GoIM interface, rendered headlessly with demo data.</sub></p>
</div>

GoIM is a desktop messenger built with Go and [Viva](https://github.com/vivaui/viva). Devices discover each other on the LAN, exchange messages and files directly, and keep their chat history locally. Recipients can open file shares in a browser.

<table>
  <tr>
    <td width="50%"><a href="website/assets/screenshots/contacts-light.png"><img src="website/assets/screenshots/contacts-light.png" alt="Grouped contacts and presence" /></a><strong>Contacts</strong></td>
    <td width="50%"><a href="website/assets/screenshots/transfers-dark.png"><img src="website/assets/screenshots/transfers-dark.png" alt="The incoming queue and active transfers in dark mode" /></a><strong>Transfers</strong></td>
  </tr>
  <tr>
    <td width="50%"><a href="website/assets/screenshots/shares-light.png"><img src="website/assets/screenshots/shares-light.png" alt="File shares and password protection in the desktop app" /></a><strong>Desktop file sharing</strong></td>
    <td width="50%"><a href="website/assets/screenshots/lan-share-directory.png"><img src="website/assets/screenshots/lan-share-directory.png" alt="LAN share directory showing file names, types and sizes, with an upload button at the top right" /></a><strong>LAN file sharing in a browser</strong></td>
  </tr>
  <tr>
    <td width="50%"><a href="website/assets/screenshots/print-sharing.png"><img src="website/assets/screenshots/print-sharing.png" alt="Three shared printers with availability, paper sizes, color and duplex options, followed by recent jobs" /></a><strong>Printer sharing</strong></td>
    <td width="50%"><a href="website/assets/screenshots/chat-light.png"><img src="website/assets/screenshots/chat-light.png" alt="Group chat and the file panel in light mode" /></a><strong>Light appearance</strong></td>
  </tr>
</table>

## Features

| Messaging | Transfers and sharing |
| --- | --- |
| Direct and group chats, invitations, and membership controls | Receive prompts, Save As, cancel, retry, and resume |
| Text, emoji, and inline images with a separate viewer for zoom, rotate, and save | TLS and SHA-256 verification between GoIM clients |
| Local history, search, unread counts, pinned conversations, and offline retry | File and folder links, QR codes, and per-project passwords |
| Contact groups, profiles, app lock with system verification, light and dark appearances, and Chinese and English UI | Device share homepage with browser uploads |

Direct chats support two-way voice calls with incoming-call acceptance, hang-up, microphone mute, and speaker controls. Choose the default microphone and speaker and adjust input or playback volume in **Settings → Calls**.

**Print Sharing** lets members share local printers and choose a file and print options in a separate window. Pin frequently used printers from their cards. Owners can set, change, or remove an optional password under **My shares → Edit share**. Protected printers require verification on first use and after a password change; a printer-specific authorization is saved locally instead of the plaintext password. Both sides retain print history. Files are not accepted or submitted to the system print queue before authorization succeeds.

Use `Alt+A` or the scissors in the composer to capture your screen. Windows and macOS 13.3 or later use the bundled Shotlet helper for annotation, scrolling capture, OCR, and pinning. Confirm a screenshot to paste it into a chat. Shotlet supports recording on Windows and macOS 15 or later. Linux and macOS 13.0–13.2 use GoIM's built-in tool. On macOS, grant screen recording permission for Shotlet on first use.

## Install

Choose your platform and architecture from [GitHub Releases](https://github.com/Ezreal-byte/GoIM/releases/latest).

| Platform | Architecture | Package formats |
| --- | --- | --- |
| Windows | x64, ARM64 | `Setup.exe`, `.zip` |
| macOS 13 or later | Intel, Apple silicon | `.dmg`, `.zip` |
| Linux (X11 or XWayland) | x64, ARM64 | `.deb`, `.tar.gz` |

Linux packages use Viva's pure-Go build path and do not require GLIBC 2.32 or 2.34. Release validation permits only the baseline libc, libpthread, and libdl dependencies and rejects symbol requirements newer than GLIBC 2.31. The system must still provide the libc, X11, XKB, Vulkan, and ALSA (`libasound2`) runtime libraries listed by the DEB dependencies. Kylin and UOS use the same compatibility build.

Linux file dialogs prefer the desktop's XDG Portal or an installed zenity/kdialog. Both `.deb` and `.tar.gz` packages include a file dialog helper and its runtime libraries, used automatically when neither system helper is installed. Installing or using the bundled helper needs no network access or extra zenity/kdialog installation. Fully extract portable packages and keep the `lib/goim/file-dialog` directory. Under **Settings → About**, you can copy a redacted diagnostic report. If the save dialog is unavailable, exporting saves the report to the default download folder and shows its path (or the app data folder's `diagnostics` subfolder if no download folder is configured).

Windows uses Viva's `Setup.exe`, which installs for the current user. Uninstall standalone MSI builds before switching to Setup. GoIM stores profiles and chat history in a separate user data folder.

For archives, extract the full package so GoIM can find its screenshot tool. Windows packages have no commercial code signature. macOS uses ad-hoc signing and requires the usual local approval for unnotarized apps. Verify downloads with the release's `SHA256SUMS`.

### Software updates

Automatic update checks are enabled by default and run at startup and every 6 hours. Failed checks retry after 30 minutes. Disable automatic checks or check manually under **Settings → About**; enabling the switch starts a check immediately. Downloading and installing updates still require user action.

Windows and Linux installers check for running GoIM instances before replacing files. Exit every instance, including its tray icon, then retry installation. In-app updates exit the current instance before the update helper installs and restarts it. Linux DEB updates continue to use the system package manager.

### Clear local data

**Settings → About → Clear all user data** requires a second confirmation. **Clear and exit** stops networking and background tasks, closes the database, then clears this device's identity key, contacts, chats, passwords, settings and app-data caches. Start GoIM again to create a new identity, which other devices will recognize as a new user. Downloaded files, shared originals and screenshot files are kept; other devices are unaffected. If shutdown is interrupted, the next launch finishes the confirmed reset.

## Quick start

1. Run GoIM on two devices on the same reachable LAN.
2. Use the search field at the top of **Contacts** to find the other device. If discovery is unavailable, choose **Connect by IP** at the bottom of the sidebar and enter its IP or `IP:47842`.
3. Send a message or image. Receive other attachments in the conversation or on the **Transfers** page.
4. Add a file or folder in **File sharing**, then copy its link or show its QR code. To share several items from one page, choose **Device sharing → Enable device page** and copy the homepage link. Anyone on the same LAN with the link can browse the directory and download files. To send you a file, use the upload button at the top right or drag the file into the directory.
5. To use a colleague’s printer, open **Print sharing**, choose an available printer with the paper, color and duplex options you need, then click **Print** to select a file. Check its status under **Recent prints**.

Native chat uses TCP 47842 and IPv4 multicast at `239.192.77.77:47841` for discovery. Browser sharing uses LAN HTTP on trusted networks.

### Discover GoIM across routed subnets

Publish a DNS SRV record for an always-on GoIM entry point and an A or AAAA record for its host. For example:

```dns
_goim._tcp.office.example.  60 IN SRV 0 0 47842 entry.office.example.
entry.office.example.       60 IN A   10.10.1.20
```

Cross-subnet discovery is off by default. To enable it, turn on automatic discovery and set the internal DNS server and discovery domain on the entry point and all participating clients under **Settings → Network → Advanced network settings → Cross-subnet discovery**. The default domain is `goim.internal`. The resolver accepts an IP or an IP and port (default 53). The setting applies only to GoIM. Once registered, clients discover other online participants and connect directly over TLS. Clients and the entry point need a version supporting this feature, a route to the resolver, and TCP access to each other's GoIM listening port. Invisible mode pauses registration. Clear the server and save to leave the directory.

## App lock and system verification

Set a GoIM lock password under **Settings → Security → App lock**, then enable **Use system verification to unlock**. Enabling it requires your current GoIM password. On the lock screen, choose **Use system verification**, or continue using the GoIM password. Canceling or failing verification leaves GoIM locked.

| System | Available system verification |
| --- | --- |
| Windows 11 | Windows Hello with a configured PIN, fingerprint, or face |
| Windows 10 | Desktop Hello verification requires Windows 11; use the GoIM password when system verification is unavailable |
| macOS | Touch ID, or the current macOS login password when Touch ID is unavailable |
| Linux | System verification is unavailable in this version. The switch is disabled with an explanation; use the GoIM lock password |

GoIM receives only the verification result and never reads, stores, or transmits system passwords or biometric data. Existing Linux system-verification settings are turned off automatically while GoIM lock passwords are kept. The bundled polkit policy does not enable this unavailable feature. Windows Hello must be enabled in Windows first; the Windows account password is not passed to GoIM as a Hello credential.

## Legacy compatibility

GoIM chooses the wire protocol from the peer. There is no per-contact compatibility selector and no manual identity-linking step.

| Peer | Route |
| --- | --- |
| GoIM | Native GoIM TLS protocol |
| Neiwangtong (内网通) | Native Neiwangtong protocol: UDP 9011 discovery, ENet 9012 control, TCP 9013 avatar and file streams |
| FeiQ (飞秋) / IP Messenger | Standard IPMSG on port 2425 |

These results apply to development on `main` after the `0.1.16` release. The table covers the original Neiwangtong `3.4.3055` client. Checks use received files, chat records, hashes or protocol receipts as appropriate.

| Capability | Status | Automated evidence |
| --- | --- | --- |
| Discovery, online and offline transitions, profile exchange, native avatar download | Verified | No manual address; the same contact was updated after the original exited and restarted. Handshake and image digests checked |
| Direct text, Chinese text, special characters, delivery receipts | Verified in both directions | Sent and received records matched with application receipts |
| Direct and group text recall | Verified in both directions | The recalled text disappeared and the neighboring control message remained |
| Online individual files, reject, recall and cancel | Verified in both directions | Real files compared by SHA-256 |
| Inline PNG, JPEG and GIF messages | Verified in both directions | Original-client cache and received bytes compared exactly |
| Directory transfer | Verified in both directions | Nested Chinese paths, empty subdirectories and zero-byte files preserved |
| Restarting an interrupted direct-chat folder transfer | Fresh offer verified in both directions | Accepting a new offer restarted the transfer from zero. Received files matched by SHA-256. Resuming from the original offer has not passed testing |
| Busy and away presence, avatar replacement | Original presence → GoIM and avatar replacement in both directions verified | The same contact changed presence in sequence. Both clients received two replacements; GoIM cleared its copy when the original restored its default avatar |
| Individual file resume | Verified in both directions | Non-zero offsets and final SHA-256 checks; GoIM retries reuse the file's original task ID |
| Offline text and file replay | Verified in both directions | Both clients restarted with isolated profiles |
| Original-client-created group chat | Text and inline PNG, JPEG, and GIF verified in both directions | Text matched the original transcript; the original cache and GoIM downloads matched the image samples |
| Neiwangtong group created in GoIM | Creation, member addition, renaming, three-client text and original-client exit verified | Two originals and GoIM exchanged messages. Renaming kept the conversation, and when one original left the other stayed in the group |
| Group dismissal and member removal | Verified against the original for GoIM-initiated actions; original commands and GoIM state also tested | The original closed the affected group window. GoIM accepts these actions only from the owner and retains local history |
| Regular group files | Verified in both directions | Files sent and accepted in a group matched the source SHA-256 on disk |
| Original built-in emoticons | Mapping for 84 IDs implemented; the smile verified in both directions | Neiwangtong receives its native symbol; GoIM renders its own emoji or descriptive text |
| Regular files larger than 4 GiB in direct chats | Verified in both directions | Each receiver saved a 4,295,032,849-byte file with the source length and SHA-256. [Windows original-client receive test](https://github.com/Ezreal-byte/GoIM/actions/runs/44755141926) |

Group folders also passed both directions against the original, preserving nested Chinese paths, empty subdirectories and zero-byte files. Group file invitations remain downloadable after GoIM restarts.

Group folders resume in both directions. GoIM can resume regular group files, and the original client can restart them from the same offer. Files sent by GoIM to other Neiwangtong versions or to Neiwangtong group chats must still be smaller than 4 GiB each. Remaining gaps include nonzero-offset resume for group files received by the original client, fully empty root directories, group ownership transfer, custom expression packs, native shared-directory browsing and remote assistance.

Original IP Messenger `5.9.1` passed automatic discovery, two-way Chinese messaging and regular file transfers. The original Windows FeiQ 2013 (`3.0.0.2`) passed unicast discovery at an explicit loopback address, two-way Chinese messaging and two-way regular file transfers with Chinese filenames. In each direction the receiver saved a 98,321-byte file with the expected SHA-256. Broadcast discovery between machines and other FeiQ versions await testing. [Windows FeiQ acceptance run](https://github.com/Ezreal-byte/GoIM/actions/runs/44756249204) · [IPMSG compatibility progress](docs/ipmsg-compatibility-progress.md)

See the detailed [Neiwangtong compatibility progress](docs/neiwangtong-compatibility-progress.md) and the [automated Neiwangtong test guide](docs/neiwangtong-testing.md). The adapters can be managed under **Settings → Network**; `--no-compatibility` starts a GoIM-only instance.

## Build from source

Use the Go version pinned by CI (currently 1.27.3). Go downloads the exact Viva revision from [`go.mod`](go.mod). When working on Viva itself, you can make a local Go workspace that includes both repositories.

```sh
go test ./... -count=1
go vet ./...
go run ./cmd/goim
```

On Windows, `pwsh -File scripts/start-temp-dev.ps1` builds and starts a client with its own profile and download directory. Add `-Loopback` to disable LAN discovery and legacy adapters, or run `scripts/start-dual.ps1` for two clients connected over loopback.

Windows and macOS packaging builds the Shotlet capture SDK helper from pinned revisions with CMake 3.20 or later. Windows needs Visual Studio C++ tools for the target architecture and the Windows SDK. Visual Studio 2026 requires CMake 4.2 or later. macOS needs the Xcode Command Line Tools. Install NSIS to build Windows Setup with `go run ./cmd/package --windows-setup`. Linux builds use the existing GoIM toolchain because Shotlet's Wayland capture and native selection overlay are incomplete. [Shotlet source](https://github.com/Ezreal-byte/shotlet)

`pwsh -File scripts/smoke-headless.ps1` builds a fresh headless client and runs a three-process test of messaging, file transfer, and browser sharing. Use `scripts/build.ps1` to build the desktop app and screenshot companion without starting them, or `scripts/promo-headless.ps1` to regenerate the chat, contacts, transfers, and desktop file sharing screenshots. See [Actions](https://github.com/Ezreal-byte/GoIM/actions/workflows/release.yml) for platform packaging and validation.

See the [release guidelines](docs/releases/README.md) for the release notes format.

## Acknowledgements

Thanks to the Viva team for its native Go desktop framework. GoIM uses Viva for its chat windows and controls, and its headless renderer for UI tests and screenshots. [Viva project](https://github.com/vivaui/viva)

Thanks to [imjakey / Shotlet](https://github.com/imjakey/shotlet) for the open-source screenshot tool. GoIM integrates a Shotlet-based capture helper on Windows and macOS.

## License

[MIT](LICENSE) · [Third-party notices](THIRD_PARTY_NOTICES.md)
