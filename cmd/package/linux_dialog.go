package main

import (
	"debug/elf"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func stageLinuxDialog(staging string) error {
	dest := filepath.Join(staging, "lib", "goim", "file-dialog")
	source := os.Getenv("GOIM_LINUX_DIALOG_RUNTIME")
	if source == "" {
		if err := run("bash", "packaging/linux/build-dialog-runtime.sh", dest); err != nil {
			return fmt.Errorf("build offline file dialog: %w", err)
		}
		return validateLinuxDialog(dest, runtime.GOARCH)
	}
	if err := validateLinuxDialog(source, runtime.GOARCH); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		// Bundles deliberately dereference distribution symlinks at build time.
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular dialog runtime entry: %s", path)
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if err := copyFile(path, target); err != nil {
			return err
		}
		return os.Chmod(target, info.Mode().Perm()&^0o022)
	})
}

func validateLinuxDialog(root, arch string) error {
	b, err := os.ReadFile(filepath.Join(root, "architecture"))
	if err != nil || strings.TrimSpace(string(b)) != arch {
		return fmt.Errorf("offline dialog runtime architecture must be %s: %q (%v)", arch, b, err)
	}
	for _, name := range []string{"bin/zenity", "zenity", "xclip", "ld.so", "gtk-query-immodules-3.0", "gdk-pixbuf-query-loaders", "packages.tsv"} {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("offline dialog runtime missing %s", name)
		}
		if name != "packages.tsv" && info.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("offline dialog runtime is not executable: %s", name)
		}
	}
	want := elf.EM_X86_64
	if arch == "arm64" {
		want = elf.EM_AARCH64
	}
	for _, name := range []string{"zenity", "xclip", "ld.so"} {
		file, err := elf.Open(filepath.Join(root, name))
		if err != nil {
			return fmt.Errorf("dialog runtime %s: %w", name, err)
		}
		machine := file.Machine
		file.Close()
		if machine != want {
			return fmt.Errorf("dialog runtime %s is %s, expected %s", name, machine, want)
		}
	}
	return nil
}
