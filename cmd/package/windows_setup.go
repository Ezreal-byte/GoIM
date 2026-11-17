package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/vivaui/viva/tools/manifest"
	"github.com/vivaui/viva/tools/winpkg"
)

// buildWindowsSetup feeds the same payload as the portable ZIP to Viva's NSIS
// packager. This keeps the GoIM build metadata, capture helper and licenses in
// all formats while using the same installer and shortcuts as viva package.
func buildWindowsSetup(staging, setup, version, arch string) error {
	config, err := manifest.Load(".")
	if err != nil {
		return err
	}
	staging, err = filepath.Abs(staging)
	if err != nil {
		return err
	}
	setup, err = filepath.Abs(setup)
	if err != nil {
		return err
	}
	const exe, ico = "GoIM.exe", "GoIM.ico"
	var files []winpkg.ExtraFile
	if err := filepath.WalkDir(staging, func(path string, file fs.DirEntry, err error) error {
		if err != nil || file.IsDir() {
			return err
		}
		relative, err := filepath.Rel(staging, path)
		if err != nil {
			return err
		}
		if relative != exe && relative != ico {
			files = append(files, winpkg.ExtraFile{Source: path, Destination: filepath.ToSlash(relative)})
		}
		return nil
	}); err != nil {
		return err
	}
	if err := winpkg.CheckExtraFiles(files); err != nil {
		return err
	}

	scratch, err := os.MkdirTemp(filepath.Dir(setup), ".goim-setup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	output := filepath.Join(scratch, filepath.Base(setup))
	result, err := winpkg.Build(winpkg.Script{
		Name: config.App.Name, Version: version, Publisher: config.Windows.Publisher,
		URL: config.Windows.URL, ID: config.App.ID, Scope: config.Windows.InstallScope(),
		Executable: exe, Program: filepath.Join(staging, exe), Icon: filepath.Join(staging, ico),
		Arch: arch, Files: files, Output: output,
	})
	if err != nil {
		return err
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(os.Stderr, "makensis: %s\n", warning)
	}
	return os.Rename(output, setup)
}
