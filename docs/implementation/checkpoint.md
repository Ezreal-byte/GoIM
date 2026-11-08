# Current checkpoint

Development now uses [Viva `b357903` plus the window-size patch](../../framework/README.md). The local sibling `../viva` is on `codex/goim-viva-upstream-sync` at `ca30ab7`; the previous integration branch remains available for reproducing older work. Viva #155 and #157 are merged. #170 was closed as superseded by the upstream preview and generated recipes. The remaining patch is for draft #172.

Verified on Windows: `go vet ./...`, all GoIM tests, the Viva hotkey and Direct2D regressions, and the three-process headless smoke including automatic images, confirmed files, unlock, browser download and QR. The light/dark, scale, chat panel and screenshot editor suites passed; captures are in `artifacts/viva-upstream-sync`. Windows ARM64 and Linux ARM64 passed cross-vet. Viva's docs and recipe generation checks passed. The six-platform packaging workflow has not been rerun for this local baseline.

Alpha.9 delivery evidence is in [optimization-checkpoint.md](optimization-checkpoint.md) and [optimization-verification.md](optimization-verification.md). The alpha.6 record below is historical.

2026-11-07 final delivery: GoIM 0.1.0-alpha.6, source ba7fb5753ca304e02f6cc59b96700644d010e9cb. All six platform builds/tests/renderers, Linux race and final release verification succeeded. See delivery.md for exact repository, CI, private Draft Release, local packages, native executable and verification limits.

Windows final packaged executable runs from build/GoIM.exe; GoIM.lnk is the normal startup entry. The firewall permission modal remains for the user; do not operate it or claim physical GUI/IME/tray/cross-machine acceptance. Native process ID is in build/native-process-id.txt; verify identity before any process control.

All six final packages are in dist/releases/alpha6 and locally hash verified. Final Windows screenshots are artifacts/final-ci-windows/screenshots, real-process screenshots artifacts/final-ci-windows/headless. Viva has separate histories/patches and two open upstream contributions; no merge performed. All workers ended. Both scheduled continuation automations deleted. No quota reset credit used.

Do not repeat completed implementation or create new automations unless requested. Next user input may request manual native acceptance after they handle OS permission, further changes, or publication; the current release is intentionally private and Draft.

User steering: Viva PR155 and PR157 are both Draft. Keep both Draft pending the user's evaluation; no automatic ready/merge actions.
