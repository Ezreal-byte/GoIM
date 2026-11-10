# GoIM implementation

Read `.agents/skills/building-with-viva/SKILL.md` before changing Viva UI. Local checkouts may include a private spec at `docs/design/interaction-spec.md` and reference images under `docs/design/images`. Use Go and native Viva components for the desktop app. Preserve the light and dark three-column design. Settings replaces contacts and chat with categories and options while retaining the outer navigation.

Core/network/storage is owned by the root GPT-6.1 Sol agent. WorkBuddy UI workers may change only their assigned files; never revert other workers' edits. Reusable desktop gaps belong in the Viva checkout; application behavior stays here. No arbitrary numeric control identities. Cache expensive presentation outside Paint. UI state refreshes through OnWake, not a repaint timer. Networking's bounded discovery heartbeat is application protocol behavior; it must not cause unchanged UI frames.

Use `internal/domain` as the shared contract. Desktop code depends on its Backend interface, not core internals. Never treat demo data as proof of real networking. All keys, database records and test data must stay outside committed source. Native integration tests use isolated data directories, not HOME overrides.

Before handoff, format changed Go files, run `go vet ./...` and `go test ./... -count=1`, and verify desktop changes in the headless harness. Record the checks performed in the local checkpoint at `docs/implementation/checkpoint.md` when available. Keep private design and implementation records outside Git.
