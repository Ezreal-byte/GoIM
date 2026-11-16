# Reproducing Viva

Check out [Viva](https://github.com/vivaui/viva) at the exact commit in `viva-commit.txt` in the sibling `../viva` directory, then apply the reviewed patches below. Both GoIM workflows and package metadata read that same pin. App and framework maintain independent Git histories.

```powershell
# Run from the GoIM root with a clean sibling Viva checkout.
$vivaRevision = (Get-Content -Raw framework/viva-commit.txt).Trim()
git -C ../viva fetch origin main
git -C ../viva switch --detach $vivaRevision
git -C ../viva -c core.autocrlf=false -c core.eol=lf checkout-index --force --all
git -C ../viva apply ../GoIM/framework/0004-windows-tray-notifications.patch
```

The pinned upstream includes [#155](https://github.com/vivaui/viva/pull/155) (global shortcuts), [#157](https://github.com/vivaui/viva/pull/157) (Windows ARM64 Direct2D text), [#170](https://github.com/vivaui/viva/pull/170) (documentation and generated component recipes), and [#172](https://github.com/vivaui/viva/pull/172) (programmatic native window sizing). Earlier pins and patches remain in Git history for reproducing alpha.9 and earlier builds.

`0004-windows-tray-notifications.patch` makes Windows desktop notifications reuse the application's first live status item. Viva still provides a fallback when an application does not create a status item. The Windows packaging jobs run the patch's regression test.
