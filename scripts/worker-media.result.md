# Media worker handoff (root integration record)

The requested workbuddy/deepseek-v4.1-flash/max worker implemented the typed, coalescing background media pipeline and its tests in app.go, panels.go and media_async*.go. It completed initial full tests, vet, screenshot checks and wake/paint benchmarks. During final verification it found a startup avatar wake that could be lost before the invalidator was bound. Its verification loop repeatedly used an unsupported shell; root stopped only that owned CLI process and completed integration.

Root fixes: remove the attempted wake-retry timers entirely; request exactly one wake after the native window binds; model the same startup lifecycle in the Driver harness; wait for a published revision rather than assuming the next coalesced wake token corresponds to that publication. Initial-avatar-without-backend-update, blocked decode/nonblocking wake, coalescing/stale-result guards, missing/deleted files, avatar stamp changes and shutdown tests pass. Closing also prevents a queued local action from launching after its preflight returns.

Results: desktop suite repeated three times passes; full application tests and vet pass; three independent-process TLS chat/file/group smoke passes. Final Linux CI also runs the desktop race tests. No native window was opened by the worker, and it made no commit.
