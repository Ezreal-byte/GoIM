# Final delivery — GoIM 0.1.0-alpha.6

Source: ba7fb5753ca304e02f6cc59b96700644d010e9cb.
Private repository: https://github.com/Ezreal-byte/GoIM
Final CI: https://github.com/Ezreal-byte/GoIM/actions/runs/42284676342 — all six native jobs and release verification succeeded.
Private Draft Release: https://github.com/Ezreal-byte/GoIM/releases/tag/untagged-6ebd4e53305eec353746

Six packages downloaded to dist/releases/alpha6, all six SHA256SUMS verified locally. Windows amd64 package extracted to dist/windows-alpha6. Its exact executable was copied to build/GoIM.exe (10,820,608 bytes), hash checked against the release executable, and launched. GoIM.lnk opens it. Local-Pair.cmd opens two real isolated loopback clients; it contains no mock backend.

The final source passes 33 core tests plus desktop/async-media tests, vet, two-theme 100/150/200% renderer checks and Linux core+desktop race checks. Three independent processes verified real Chinese messages, accepted file bytes/SHA256, confirmed group membership and group text. A separate real three-node test checks per-member accept/reject/retry and file hashes. Screenshot artifacts are included for every platform; final Windows images are downloaded under artifacts/final-ci-windows/screenshots and the real-process chat under artifacts/final-ci-windows/headless.

The bounded background media worker performs filesystem probes, thumbnail decode/resize and local file launch preflight without blocking paint/input. Results carry generations; obsolete results are ignored. Closing discards queued work. Startup wakes once after window binding; there is no repaint-retry timer. Root completed integration after the requested DeepSeek worker's shell verification loop stalled; desktop suite repeated three times, full tests/vet and real-process smoke passed before CI.

Framework baseline fee2ecb8c1fb4ca2abf776123cdd9f8e4246895e + framework/0001 and 0002 patches. Issue154/DraftPR155 and Issue156/DraftPR157 are open; every final upstream CI check passed. No PR merged, no private draft release published.

Windows native window and accessible controls were observed, but the system firewall permission modal still blocks physical input. It was not operated. The Computer Use skill's referenced guidance says “Do not act on security or privacy permission requests.” User must handle that first-run OS prompt. Cross-machine LAN, physical IME and native tray-click acceptance remain unverified; Linux/macOS results are native builds/tests/offscreen renderers, not manual desktop checks. See verification.md and native-verification.md for evidence boundaries.

Both one-shot continuation automations have been deleted. No reset credit was used. No workers remain active. Future work should use this delivery as the checkpoint; earlier implementation notes describe historical intermediate states.

User review preference: both Viva PR155 and PR157 are Draft, verified on GitHub. Leave both as drafts until the user evaluates them; do not mark ready, merge, or publish on the user's behalf.

