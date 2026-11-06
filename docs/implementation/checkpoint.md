# GoIM execution checkpoint

Updated: 2026-11-07 00:51 Asia/Shanghai.

- Stage: core implemented and tests pass; desktop UI workers still implementing their owned files. No executable built yet.
- Automation: goim at 01:30 and goim-2 at 06:00, bound to this chat, one scheduled occurrence each.
- Main model: GPT-6.1 Sol. UI and review workers: workbuddy/deepseek-v4.1-flash, max, via codex exec (probe verified).
- Viva clean baseline: c59ef885063db72ef197e0bb5f5f292c60665f1c, sibling ../viva.
- Local Go toolchain: 1.27.3 windows/amd64. CGO disabled, no compiler; race checks belong in Linux CI.
- Baseline verification: viva headless/... and examples/guides/hello tests passed.
- Core and framework owned by root. Desktop shell/chat worker and panels/settings worker must own separate files.
- Contract: internal/domain/types.go, Backend Snapshot/Submit/Updates. Snapshot refresh on OnWake, not per-paint.
- Commits: 0296208 (design/contracts), 01fc087 (real persistent TLS engine, group operations and resumable files + main entry).
- PASS: go test ./internal/core -count=1 -timeout=100s -v. Five integration tests cover real TLS messaging/dedup/restart/offline replay, resumed file bytes/hash, 3-member group without owner, ownership transfer and member removal, private-path-free shared file catalog and rejection.
- Active CLI workers (exact WorkBuddy model/max): shell session 26826, thread 01a1b1ad-fc5c-7073-b5bd-59f56903c5d8; panels session 65150, thread 01a1b1ad-fc6c-7183-918e-4cc7d4cc6bd0; read-only core reviewer session 96066. Logs scripts/worker-{shell,panels,review}.jsonl. They are EPHEMERAL: codex queue cannot address them, so inspect logs and existing processes; do not assume CLI thread IDs can be resumed. PTY session IDs can be polled by write_stdin within this tool session.
- Shell has app.go (~56KB) but still owes chat/groups/icons/tests. Panels has its 3 source files and tests, generating native offscreen QA. Root must wait for files before compiling whole application; current compile failure is incomplete shell methods, not core.
- CRITICAL integration TODO: shell must forward Panels.Key, Panels.Shortcut and Panels.Wheel for panel sections. These methods exist in panels.go, but current shell handlers return empty for panel input. Do not change worker-owned files while they are writing them.
- Root next: inspect finished reviewer findings, fix concrete engine issues; finish native runtime/tray/notifications; integrate complete desktop, go test/vet; build vivahead, use tree/layout/shot and compare actual screen. Add tests for cancellation races and bad file paths; enforce monotonic terminal transfer status so late progress cannot override completed/cancelled/rejected.
- Known follow-ups: network setting is currently persisted but doesn't recreate the multicast listener until restart; receiver state completion can race sender's final verifying status; default GUI main currently RunCanvas, so tray runtime still needs RunApplication; global shortcut registration not yet implemented; avatar propagation not yet implemented. Read-only reviewer may find additional issues. Do not claim complete until resolved.
- Later: create private Ezreal-byte/GoIM, CI six native platforms, private draft release. No repo or release has been created yet.
- Quota checked around 00:45: 97% of 5-hour window consumed; resets 01:16:07 Asia/Shanghai. Do not spend the user's optional reset credit. Automatic wakeups already created for 01:30 and 06:00.
- Quota now reports 100% (ordinaryUsageAllowed still true); main should resume after reset via 01:30 heartbeat. Last root fix commit 9378c1a: ownership consent bound to the target group; terminal-transfer late-progress guard and file ID/path validation regression tests.
- Additional PASS: full original core integration suite repeated 20 times (30 seconds), then core suite including two safety tests passed.
- Panels worker COMPLETE: panels.go/settings.go/transfers.go/panels_test.go; its targeted Driver tests and vet pass, it rendered offscreen QA. It exposes Panels.Key/Shortcut/Wheel; main shell MUST forward these. Network enumeration is init-only; active-network deferral and OS global shortcut registration remain integration tasks.
- Shell worker still active, all app.go/chat.go/groups.go/icons.go now exist; it is fixing exact Viva API mismatches and adding tests. Do NOT edit these while it runs. Its main file was ~63KB when last observed. Reviewer still active.
- Attempts to queue updates to ephemeral workers failed (no rollout); no instructions were delivered. Future CLI workers needing follow-up should omit --ephemeral. Root must apply panel forwarding after shell finishes.

Do not restart an existing worker. Inspect scripts/worker-*.jsonl and running codex processes first. Normal launch must use genuine peer data; only explicitly labelled demo/test fixtures may seed sample contacts.
