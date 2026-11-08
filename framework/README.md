# Reproducing Viva

Check out [Viva](https://github.com/vivaui/viva) at the exact commit in `viva-commit.txt` in the sibling `../viva` directory, then apply `0003-window-size.patch`. Both GoIM workflows and package metadata read that same pin. App and framework maintain independent Git histories.

```powershell
# Run from the GoIM root with a clean sibling Viva checkout.
$vivaRevision = (Get-Content -Raw framework/viva-commit.txt).Trim()
git -C ../viva fetch origin main
git -C ../viva switch --detach $vivaRevision
git -C ../viva -c core.autocrlf=false -c core.eol=lf checkout-index --force --all
git -C ../viva apply ../GoIM/framework/0003-window-size.patch
```

The pinned upstream already includes [#155](https://github.com/vivaui/viva/pull/155) (global shortcuts) and [#157](https://github.com/vivaui/viva/pull/157) (Windows ARM64 Direct2D text). It also contains the documentation preview and generated component recipes covered by [#170](https://github.com/vivaui/viva/pull/170). Earlier pins and patches remain in Git history for reproducing alpha.9 and earlier builds.

`0003-window-size.patch` is the remaining local change. It adds `app.Window.SetSize` for Windows, AppKit and X11 so GoIM can make room for chat files. The window manager reports the granted size through the existing resize callback. Other hosts can fit the panel inside the existing frame. Its upstream [draft #172](https://github.com/vivaui/viva/pull/172) is still open. Update the pin and remove the patch after the API is upstream and verified.
