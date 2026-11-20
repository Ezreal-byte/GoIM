package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// A separate decoder keeps codec code out of the GUI process and its idle
// footprint. Ship its exact LGPL source alongside the compiled program.
func stageMedia(staging, binDir string) error {
	bundle := os.Getenv("GOIM_MEDIA_BUNDLE")
	if bundle == "" {
		bundle = filepath.Join(".runtime", "media-"+runtime.GOOS+"-"+runtime.GOARCH)
		if runtime.GOOS == "windows" {
			if err := run("wsl", "--", "bash", "packaging/media/build.sh", "windows", runtime.GOARCH, filepath.ToSlash(bundle)); err != nil {
				return fmt.Errorf("build Windows decoder in WSL or set GOIM_MEDIA_BUNDLE: %w", err)
			}
		} else if err := run("bash", "packaging/media/build.sh", runtime.GOOS, runtime.GOARCH, bundle); err != nil {
			return err
		}
	}
	name := "goim-media"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	src, dest := filepath.Join(bundle, name), filepath.Join(binDir, name)
	if err := verifyArchitecture(src); err != nil {
		return fmt.Errorf("media helper: %w", err)
	}
	if err := copyFile(src, dest); err != nil {
		return err
	}
	if err := os.Chmod(dest, 0755); err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		if err := run("codesign", "--force", "--sign", "-", dest); err != nil {
			return err
		}
	}
	if err := run(dest, "-v", "error", "-version"); err != nil {
		return err
	}
	for _, asset := range []string{"ffmpeg-8.1.3.tar.xz", "FFmpeg-LGPL-2.1.txt"} {
		if err := copyFile(filepath.Join(bundle, asset), filepath.Join(staging, "licenses", "ffmpeg", asset)); err != nil {
			return err
		}
	}
	return copyFile("packaging/media/build.sh", filepath.Join(staging, "licenses", "ffmpeg", "build.sh"))
}
