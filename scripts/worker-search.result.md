**Completed**
- Added independent in-chat find bar with header icon and configured `Ctrl+F`; filters current-chat message text/file names, shows real match count, supports close/Esc with scroll restoration. `Ctrl+K` remains sidebar-only. See `internal/desktop/chat_search.go:40` and `internal/desktop/chat.go:136`.
- Added atomic, default-visible `UI.SetWindowVisible`, with `MarkRead` emitted only for a visible selected chat outside settings/transfers/shares and duplicate-submit protection. See `internal/desktop/app.go:609`.
- Added pre-decode file-byte and `DecodeConfig` pixel limits, plus mtime+size avatar cache invalidation. See `internal/desktop/app.go:2131` and `internal/desktop/app.go:2258`.
- Added Driver tests for search separation, filtering, scroll restoration, unread visibility/panel behavior, and crafted oversized PNGs.

**Verification**
- `gofmt` on changed Go files
- `go vet ./...` passed
- `go test ./... -count=1` passed
- Headless screenshot/layout QA passed at 1440×960 and 1000×700; search screenshot available at `C:\Users\M\AppData\Local\Temp\goim-qa-final-1793989358836\chat-search.png`

No missing piece in the owned files. Runtime call sites for show/hide/minimize must still invoke `SetWindowVisible` as specified; no native window was opened.