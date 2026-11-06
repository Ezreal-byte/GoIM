# GoIM implementation

Read `.agents/skills/building-with-viva/SKILL.md` before changing Viva UI. The accepted product spec is `docs/design/interaction-spec.md`; reference images are in `docs/design/images`. Build native Go + Viva, not a web prototype. Preserve the selected light/dark neutral-blue three-column design. Settings replaces contacts and chat with categories and options; the outer navigation remains.

Core/network/storage is owned by the root GPT-6.1 Sol agent. WorkBuddy UI workers may change only their assigned files; never revert other workers' edits. Reusable desktop gaps belong in the Viva checkout; application behavior stays here. No arbitrary numeric control identities. Cache expensive presentation outside Paint. UI state refreshes through OnWake, not a repaint timer. Networking's bounded discovery heartbeat is application protocol behavior; it must not cause unchanged UI frames.

Use `internal/domain` as the shared contract. Desktop code depends on its Backend interface, not core internals. Never treat demo data as proof of real networking. All keys, database records and test data must stay outside committed source. Native integration tests use isolated data directories, not HOME overrides.

Before handoff: gofmt changed Go files, go vet ./..., go test ./... -count=1, headless behavior/layout checks and screenshots. Record what was actually run. Continue autonomously from docs/implementation/checkpoint.md after interruptions. Git identity is configured locally only.
