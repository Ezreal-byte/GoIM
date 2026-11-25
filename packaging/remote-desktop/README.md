# Remote desktop helper releases

GoIM packages a fixed helper component from an independent GitHub Release such as
`goim-rd-v0.1.0`. Ordinary GoIM releases download and verify that component and
run the existing integration tests. Rust, vcpkg and native codec compilation run
only in the helper release workflow.

`version.txt` holds the component version. `release.lock.json` pins its source
commit, protocol, complete build recipe, archive checksums and manifest checksums
for all six desktop targets. Each archive includes the executable, corresponding
source, AGPL license, third-party notices and manifest.

## Updating the helper

1. Update the source bundle and checkout script, or the compiler/dependency
   recipe in `.github/workflows/remote-desktop-build.yml` and
   `packaging/remote-desktop/build-linux.sh`.
2. Increase `packaging/remote-desktop/version.txt` and commit the changes.
3. Run the **Remote desktop helper release** workflow on that commit, or push
   the matching `goim-rd-v<VERSION>` tag. An ordinary GoIM `v<VERSION>` tag
   uses the helper pin without compiling the helper.
4. Download `release.lock.json` from the resulting component release and
   commit it as `packaging/remote-desktop/release.lock.json`.
5. Run `python3 scripts/remote-helper.py verify-lock` and the GoIM release
   verification before tagging the next GoIM version.

The first component release can promote a successful historical helper push run
using the workflow's `source_run` input. Its repository, workflow, source bundle,
full build recipe and six unexpired artifacts are verified before reuse.
Subsequent component releases build their own versioned artifacts.

An already published component version is verified and reused on repeated
workflow invocations. Source or recipe changes require a new component version.
Helper releases use `--latest=false` so GoIM remains the client release shown as
Latest. Release assets persist independently of the temporary Actions artifacts.

## Integrity checks

`scripts/remote-helper.py fetch --target windows-amd64 --output .runtime/remote-helper`
downloads the exact asset named in the committed pin, checks its archive and
manifest digests, and verifies the executable, corresponding source, target,
protocol, component version and build recipe. The GoIM packager performs its
existing architecture and content checks again.

The component recipe covers the source bundle, checkout script, Linux build
script and all compiler jobs in the reusable build workflow. GoIM UI, application
code and GoIM version changes do not invalidate it. A stale pin fails early and
identifies the need to publish the changed component.