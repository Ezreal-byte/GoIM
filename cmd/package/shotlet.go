package main

import (
	"fmt"
	"path/filepath"
	"runtime"

	"goim/internal/shotlet"
)

func stageShotlet(staging, goExecutable string) error {
	helper := shotlet.HelperPath(goExecutable)
	if helper == "" {
		return nil
	}
	helper, err := filepath.Abs(helper)
	if err != nil {
		return err
	}
	licenses, err := filepath.Abs(filepath.Join(staging, "licenses", "shotlet"))
	if err != nil {
		return err
	}
	if err := run("cmake", "-DGOIM_SHOTLET_ARCH="+runtime.GOARCH,
		"-DGOIM_SHOTLET_HELPER="+filepath.ToSlash(helper),
		"-DGOIM_SHOTLET_LICENSES="+filepath.ToSlash(licenses),
		"-P", "packaging/shotlet.cmake"); err != nil {
		return fmt.Errorf("build Shotlet capture helper with CMake and the native C++ toolchain: %w", err)
	}
	if err := verifyArchitecture(helper); err != nil {
		return err
	}
	if err := shotlet.SelfTest(helper); err != nil {
		return fmt.Errorf("verify staged Shotlet capture helper: %w", err)
	}
	fmt.Println("Verified Shotlet capture helper:", helper)
	return nil
}
