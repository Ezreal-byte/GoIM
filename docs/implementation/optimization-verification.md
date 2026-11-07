# GoIM optimization verification

The Windows app retains the four modules. Contacts include the current device; local contact groups can be created, moved, renamed, or removed through context menus. Deleting a chat removes its local history and hides its session, while leaving group membership and file transfer records intact. New incoming messages reopen that session. The expanded Settings pages use categories observed in the installed 内网通 help material, without adding new app modules.

The Files page browses the local filesystem on a worker and shows active shares beside it. Browser links expose one deliberately selected file and are restricted to reachable LAN addresses. A protected share is excluded from the native peer catalog; setting a password invalidates previously issued native grants and browser sessions. The password and app-lock secret are stored as salted hashes. The HTTP server validates source content before a download and supports Range requests. It never serves a directory. Revocation prevents subsequent requests. The app lock hides the interactive workspace and notification text without suspending background reception; it is not at-rest database encryption.

## Windows evidence

- `scripts/smoke-headless.ps1 -BrowserScreenshots -AvatarSample`: three independent local processes. Real TLS text and emoji, transferred PNG displayed inline, file hash, group chat, first-use lock and unlock, protected HTTP link, browser download hash, QR dialog, responsive Chrome verification at 390 px. See `artifacts/optimization/final-real`.
- The final avatar run chose a generated 5.13 MiB JPEG and saved a canonical 192 × 192 PNG. The headless working set was 106.0 MiB before preview and 109.1 MiB after preview. The subsequent 850 ms idle interval measured 0.0 ms process CPU. This is a smoke measurement on this machine, not a limit on all image imports.
- `go vet ./...` and `go test ./... -count=1` pass. Viva headless screenshot runs cover light/dark, minimum/normal window geometry, and 100/150/200 percent display scale. The latest framework checks and binaries use Go 1.27.3.
- The shortcut field border was inspected at 150 percent; the dedicated shortcut layout passed at 125, 150, and 200 percent.
- The stripped Windows executable measured 10,790,400 bytes at the previous GoIM checkpoint and 14,673,408 bytes after the application and upstream framework changes, before adding the package icon. Four isolated headless launches measured 157.8–353.1 ms to ready and 24.3–24.7 MiB idle before, versus 117.8–130.8 ms and 25.2–25.7 MiB after. These local samples include the combined dependency and framework update and do not isolate the QR library.

## Framework

- [Viva #155](https://github.com/vivaui/viva/pull/155): macOS Carbon and Linux/X11 support, Windows lifecycle review. Ready for review at `ce0bcad`. [Six native lifecycle jobs](https://github.com/Ezreal-byte/GoIM/actions/runs/42394586872) passed; Linux checks use actual X11 chord delivery in Xvfb. macOS checks exercise native registration and application handler dispatch; they do not inject physical desktop keys. Native Wayland remains unsupported.
- [Viva #157](https://github.com/vivaui/viva/pull/157): reviewed and ready at `630a7d4`; the translated glyph regression is included in upstream Windows CI. The six-platform private build also verifies native Direct2D origins on both Windows architectures.
- Both PRs include Viva upstream `2b2ce2018a0da28b20884bcec3f48cc5735042b9`. The missing tree-navigation preview is reproducible on clean upstream. It is separate from the targeted native checks. No PR was merged.

No physical phone, separate LAN device, or manual non-Windows desktop test was used. Browser functionality was tested with real local HTTP and Chrome mobile emulation, and network chat with isolated loopback processes.

[Final six-platform CI](https://github.com/Ezreal-byte/GoIM/actions/runs/42395220740) passed. The six packages, SHA256SUMS and release notes were checked locally and uploaded to the [private alpha.8 draft](https://github.com/Ezreal-byte/GoIM/releases/tag/untagged-ace4a8df9e8a90e0722f); the uploaded SHA256 digests match. The packaged source is `2441cdda0c00e0d9465e29bb364afae2afb1b33c` on `codex/goim-optimization`.
