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

## Additional Windows ARM64 renderer fix

Issue https://github.com/vivaui/viva/issues/156 and PR https://github.com/vivaui/viva/pull/157 independently fix Direct2D's by-value two-float origin ABI. Code commit 09fe72d, documentation 73980a7; local integration branch contains the fix at 4d752c5. The ARM64 target's cached typed function adds 8 bytes per render target; x64 target storage is unchanged. Native ARM64 real-renderer frames at both themes and all three scales plus the translated-ink regression now pass. Native ARM64 rasterization benchmark reports 295703–349122 ns/op, 358–380 B/op and 10 allocs/op. The previous native ARM64 path crashes, so no prior timing is valid.

The x64 stripped hello grows +512 bytes (4,280,320 → 4,280,832); x64 rasterization remains 5 allocs/op. Three short loaded samples measured 159327–159972 ns/op before and 163917–166752 ns/op after. Full local integrated framework tests pass except the independently confirmed baseline symlink-privilege fixture. The first upstream Darwin run hit an unrelated hdiutil Resource busy fixture failure; contributor rerun was denied for missing repository admin rights. The subsequent normal CI run following the evidence documentation commit passed every check, including Darwin, cross-vet, native platforms and benchmarks. No framework PR was merged by this task.
