# Viva extension evidence

Base: fee2ecb8c1fb4ca2abf776123cdd9f8e4246895e. Separate branch: codex/global-shortcut-registration. Issue: https://github.com/vivaui/viva/issues/154. Draft PR: https://github.com/vivaui/viva/pull/155.

The extension adds optional system-wide shortcut registration and private Driver simulation. Windows is implemented; Linux/macOS explicitly return ErrUnsupported. Applications without this opt-in feature create no registration or workers. No GoIM product behavior is in the framework patch.

Windows amd64 / Ryzen 7 7735HS / Go 1.27.3:

- app and headless regression suites pass.
- Opt-in real RegisterHotKey conflict/release/re-registration test passes. It does not synthesize input.
- BenchmarkDriverGlobalShortcutRegistration: 93.87 ns/op, 88 B/op, 4 allocs/op. This is new opt-in setup work, outside paint/input dispatch paths.
- Stripped `examples/guides/hello` binary before and after: 4,280,320 bytes each, measured with -trimpath -ldflags '-s -w -H=windowsgui' on a clean base worktree and extension worktree.
- Full framework suite on the Windows host has an existing toolchain-install fixture that requires symlink privileges this account lacks. The other signing-temp test had a transient access-denied failure under parallel load and passed isolated rerun. Full rerun excluding only the symlink fixture is recorded separately.
- Native GoIM window and accessible controls were observed. The Windows firewall permission dialog blocks further automated desktop input; no security prompt was operated and no firewall setting was changed. End-to-end show/hide physical key input remains unverified. Driver callback behavior and actual OS registration/release are separately tested.

The full suite excluding only the symlink-privilege fixture passed on rerun. Windows, Linux and Web vet targets passed. Darwin vet from Windows cannot resolve the Objective-C native application/mainpump and is left to the native macOS runner. The GitHub connector could read issues but returned 403 on issue creation; the user's explicitly authorized logged-in gh CLI successfully created Issue 154 and Draft PR 155.

The patch is supplied for upstream review; no automatic merge.
