**Touched**
- `internal/desktop/panels.go:369`: wake-time local-path cache using own `SourcePath` or latest completed receive `Path`; stats before caching; network mediation state.
- `internal/desktop/settings.go:320`: deferred-transfer confirmation, explicit adapter refresh, and pending-network status without claiming immediate application.
- `internal/desktop/transfers.go:246`: Open/Reveal use cached availability and are disabled for missing files.
- `internal/desktop/panels_test.go:348`: coverage for adapter entry/refresh, active-transfer defer/cancel, downloaded remote shares, deleted own shares, stale-path guards, and offscreen layout.

**Validation**
- `gofmt` on all changed Go files.
- `go test ./internal/desktop -run '^TestPanel' -count=10` passed.
- `go test ./... -count=1` passed.
- `go vet ./...` passed.
- Generated 12 offscreen light/dark screenshots across 1000×700 and 1440×960 at 1×/1.5×/2×; inspected representative light and dark Settings frames.

**Handoff**
- Root still needs to call `u.panels.OnWake(u.state)` from `UI.refresh` so the initial snapshot and every wake populate the local-path cache.
- Adapter refresh works on panel-to-panel Settings entry and the explicit “刷新网络适配器” button. Root may also call `u.panels.refreshNetworks()` in `setSection(settings)` for automatic chat→Settings refresh.
- Immediate network switching is intentionally absent because core only supports `PendingNetwork`; the prompt offers defer/cancel and tells users to cancel transfers first.