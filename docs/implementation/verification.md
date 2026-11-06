# Verification ledger

This ledger distinguishes real communication, renderer snapshots and desktop checks. Visual fixture data never backs the production app.

## Local Windows amd64

- Full application `go test ./... -count=1` and `go vet ./...` pass, with 31 core integration/regression tests and 59 desktop tests before the final asynchronous media change.
- `scripts/smoke-headless.ps1` uses three independent real processes with isolated identities/databases/downloads and loopback TLS. It sends Chinese text, confirms a real file and compares SHA256, creates a group, accepts both invitations, sends a group message and checks clean layout. The script waits for actual group controls and three accepted online members, pumping pending invalidations rather than matching the invitation's same title.
- Light/dark 1440×960 and 1000×700 at 100%/150%/200% use the real painter and layout diagnostics. Local exported frames are in artifacts/final-ui; independent-process frames are in artifacts/headless.
- Real default native window and accessible controls were observed. A Windows firewall network-permission dialog blocks further desktop input. No security dialog was operated or firewall setting changed. The referenced Computer Use skill (`C:/Users/M/.codex/plugins/cache/openai-bundled/computer-use/26.1103.31037/skills/computer-use/SKILL.md`) requires its guidance, which states: “Do not act on security or privacy permission requests.” Native tray clicks, physical IME and cross-machine discovery remain unverified. The app is built and runnable; first-run OS permission is for the user to handle.

## CI findings and fixes

1. Windows checked out upstream Viva sources as CRLF while the reviewable patch uses LF. CI now forces LF checkout of the exact baseline before applying patches; app .gitattributes fixes source/patch line endings.
2. Linux's base image lacks CJK fonts; install fonts-noto-cjk for actual Chinese text metrics and the renderer. This is also documented as a Linux runtime prerequisite.
3. macOS offscreen rendering needs the `vivahead` build tag. The renderer suite now uses that tag, and macOS ARM64 snapshots pass.
4. Native Windows ARM64 crashed in Direct2D's by-value text origin call. The generic ABI fix uses a cached typed call for its floating-point aggregate; native ARM64 snapshots and the ink-origin regression now pass. Upstream issue156 / PR157 contain the independent fix.
5. An initial empty shared catalog could overtake a newer catalog and hide a share. Catalog revisions are now persisted and stale snapshots ignored, with a deterministic reversed-order test and repeated actual-transfer regression.
6. Go tests run from their package directory; screenshot output uses an absolute workspace path so every platform's frames are included in the QA artifact.

## Core recovery and authority

Tests cover durable message/outbox commits and failure rollback, ID deduplication/replay order, restart/offline delivery, pinned TLS identity, avatars, group invitations/removal, owner offline, signed ownership transfer, former-owner rollback prevention, target leave during handoff, expired/replaced offers, file consent, remote path stripping, per-recipient cancellation, tombstones, denied retry acknowledgement, unshare revocation, resumed bytes/hash, changed source, atomic no-overwrite install, offline recovery after installation before completion, and terminal progress races. Linux CI also runs `-race`.

## Framework contributions

- https://github.com/vivaui/viva/issues/154 / https://github.com/vivaui/viva/pull/155 — optional Windows global registration and Driver simulation; all upstream checks passed.
- https://github.com/vivaui/viva/issues/156 / https://github.com/vivaui/viva/pull/157 — Windows ARM64 Direct2D origin ABI. Native ARM64 renderer/regression passed in the application matrix. Upstream checks are followed separately.

Framework base fee2ecb8c1fb4ca2abf776123cdd9f8e4246895e, two independent patches. Local integration branch codex/goim-framework-integration at 4d752c5. Windows AMD64 hello increases from 4,280,320 to 4,280,832 bytes for the ARM64 ABI abstraction (+512 bytes); ARM64 stores one function value per render target (+8 bytes). AMD64 rasterizer stays at 5 allocs/op; ARM64's corrected path reports 10 allocs/op and 295703–349122 ns/op on the native CI host. The prior ARM64 path crashed, so there is no valid prior timing.

## Delivery status

Private repository: https://github.com/Ezreal-byte/GoIM. Final six-platform run, checksums, draft release and downloaded Windows package are recorded in the checkpoint and final delivery note once the run completes. Do not infer completion from queued or partly green runs. Non-Windows real desktop checks and cross-machine networking are explicitly outside the current machine's observed evidence.
