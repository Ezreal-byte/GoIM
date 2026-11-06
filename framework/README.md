# Reproducing Viva

Checkout https://github.com/vivaui/viva at the exact commit in viva-commit.txt into the sibling ../viva directory. Run `git apply ../GoIM/framework/0001-global-shortcuts.patch` from that checkout. App and framework maintain independent Git histories.

The patch adds optional Windows system-wide shortcut registration, private Driver simulation and lifecycle tests. Other desktop backends return ErrUnsupported. Product protocol/storage/UI remain in GoIM. The main application works without configuring a global shortcut.

Do not replace the pinned commit with a moving branch. After an upstream merge, update the pin and remove the corresponding patch only after rerunning verification.
