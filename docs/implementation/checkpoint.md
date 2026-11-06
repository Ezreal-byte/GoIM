# GoIM execution checkpoint

Updated: 2026-11-07 00:25 Asia/Shanghai.

- Stage: bootstrap and shared interfaces. No user-facing executable yet.
- Automation: goim at 01:30 and goim-2 at 06:00, bound to this chat, one scheduled occurrence each.
- Main model: GPT-6.1 Sol. UI and review workers: workbuddy/deepseek-v4.1-flash, max, via codex exec (probe verified).
- Viva clean baseline: c59ef885063db72ef197e0bb5f5f292c60665f1c, sibling ../viva.
- Local Go toolchain: 1.27.3 windows/amd64. CGO disabled, no compiler; race checks belong in Linux CI.
- Baseline verification: viva headless/... and examples/guides/hello tests passed.
- Core and framework owned by root. Desktop shell/chat worker and panels/settings worker must own separate files.
- Contract: internal/domain/types.go, Backend Snapshot/Submit/Updates. Snapshot refresh on OnWake, not per-paint.
- Next: launch UI workers after publishing desktop worker contract; implement persistence, TLS peer engine, message/group/file operations, then integrated headless verification.
- Later: create private Ezreal-byte/GoIM, CI six native platforms, private draft release. No repo or release has been created yet.

Do not restart an existing worker. Inspect scripts/worker-*.jsonl and running codex processes first. Normal launch must use genuine peer data; only explicitly labelled demo/test fixtures may seed sample contacts.
