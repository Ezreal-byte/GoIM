**Implemented**
- Self-profile editor from the rail avatar: nickname validation, native `app.OpenFiles` picker, avatar preview, and `SetProfile{Name, Value}`.
- Group `@mention` autocomplete with filtering, Up/Down selection, Enter insertion, Esc dismissal, and safe Shift+Enter/IME handling.
- Unicode text emoji picker inserting at the caret without sending.
- Group nickname editing and display, with `Message.Nickname` precedence and `State.GroupNicknames` cached on wake.
- `ToID` invitation filtering, owner withdrawal, rejected-invite re-request, and configured application shortcuts.
- Avatar decoding for self and peers occurs during wake/cache refresh, outside Paint.

Core implementation is in `internal/desktop/extras.go:86`; focused coverage is in `internal/desktop/extras_test.go:38`.

**Verification**
Ran successfully:
- `gofmt` on all six owned Go files
- `go test ./internal/desktop -count=1`
- Focused profile/avatar/emoji/mention/nickname/invite/shortcut tests
- `go vet ./...`
- `go test ./... -count=1` — all packages passed
- Minimum-window layout checks
- `TestShellScreenshotQA`; visually inspected profile, emoji, mention, and group-member captures

Final screenshots: `C:\Users\M\AppData\Local\Temp\goim-desktop-qa-ebd5be64286f426cbb24c085b192a226`

**Limitations**
- The native OS picker was not visually exercised; the Driver verified its request/options and selected path handling.
- Real-window native text overlays, IME, and avatar rendering were not inspected. Offscreen screenshots cannot capture the native editing overlay.
- Avatars decode PNG/JPEG/GIF only; WebP and BMP remain unsupported for profile avatars.
- Global show/hide shortcut registration remains root/OS-owned.
- Both configured search shortcuts currently focus the existing sidebar search rather than providing a separate in-conversation find bar.
- Withdraw/re-invite state reflects after the backend wake/state refresh.