**Implemented**
- Fixed scoped dirty group-name handling, ownership-transfer wording/actions, non-owner member actions, modal shortcut safety, and IME-safe Ctrl+Enter in `internal/desktop/app.go` and `groups.go`.
- Added recipient-labelled multi-transfer progress, transfer-ID-specific controls, confirmed batch cancellation, and real `group:<id>` transfer matching.
- Fixed failed-invitation retry, group-creation reconciliation, invitation member lists, leave/dissolve transfer cancellation, clipboard-image error reporting, file-drop modal guards, and unread/rail badge behavior.
- Added Driver regressions for every assigned finding and changed the scale animation wait to 700ms in `scale_test.go`.

**Verification**
- `gofmt -l` on all changed files: clean.
- `go vet ./...`: passed.
- `go test ./... -count=1`: passed, including core and desktop.
- `TestDesktopScaleMatrix`: all 12 light/dark, 100/150/200%, 1000×700/1440×960 cases passed.
- Screenshots: `C:\Users\M\AppData\Local\Temp\goim-scale-final-7afe8965d2334c0384727e1da60b4f7c`.

Only the assigned desktop files and tests were changed. Findings 5, 11, and 13 remain with their respective owners. No native windows were launched and no commit was made.
